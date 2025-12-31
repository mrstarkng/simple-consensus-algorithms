package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

const (
	Port   = ":8080"
	DBPath = "./ledger.db"
	WebFolder = "dashboard/static"
)

// --- TYPES ---
type Event struct {
	SourceNode string `json:"source_node"`
	EventType  string `json:"event_type"`
	Message    string `json:"message"`
	Color      string `json:"color"`
	Timestamp  int64  `json:"timestamp"`
	BlockHash  string `json:"block_hash,omitempty"`
}

type SystemState struct {
	mu           sync.RWMutex
	NodeViews    map[string]int64 // Lưu View hiện tại của từng Node
	CurrentView  int64            // View thống nhất (Majority)
	ChainLength  int
}

var state = SystemState{
	NodeViews: make(map[string]int64),
}

type BlockRecord struct {
	Sequence  int    `json:"sequence"`
	PrevHash  string `json:"prev_hash"`
	Hash      string `json:"hash"`
	Timestamp string `json:"timestamp"`
}

// --- GLOBALS ---
var (
	upgrader   = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	clients    = make(map[*websocket.Conn]bool)
	broadcast  = make(chan Event)
	mu         sync.Mutex
	db         *sql.DB
	// Trạng thái: "honest" hoặc "malicious"
	nodeStatus = map[string]string{
		"node1": "honest", "node2": "honest", "node3": "honest", "node4": "honest", "node5": "honest",
	}
)

func main() {
	godotenv.Load()
	initDB()
	defer db.Close()

	http.Handle("/", http.FileServer(http.Dir("dashboard/static")))
	http.HandleFunc("/ws", handleConnections)
	http.HandleFunc("/api/report", handleReport)
	
	http.HandleFunc("/api/control/start", handleStartConsensus)
	http.HandleFunc("/api/control/reset", handleResetSystem)
	http.HandleFunc("/api/control/config", handleNodeConfig)
	
	http.HandleFunc("/api/ledger", handleGetLedger)
	http.HandleFunc("/api/explain", handleAskGemini)

	go handleMessages()
	log.Printf("pBFT Dashboard running on http://localhost%s", Port)
	log.Fatal(http.ListenAndServe(Port, nil))
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", DBPath)
	if err != nil { log.Fatal(err) }
	query := `CREATE TABLE IF NOT EXISTS blocks (sequence INTEGER PRIMARY KEY, prev_hash TEXT, hash TEXT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP);`
	db.Exec(query)
}

func saveBlock(seq int, hash string) {
	var prevHash string = "Genesis-0000"
	db.QueryRow("SELECT hash FROM blocks WHERE sequence = ?", seq-1).Scan(&prevHash)
	db.Exec("INSERT OR IGNORE INTO blocks(sequence, prev_hash, hash) VALUES(?, ?, ?)", seq, prevHash, hash)
	fmt.Printf("--> [DB] Saved Block #%d\n", seq)
}

// --- HANDLERS ---

func handleStartConsensus(w http.ResponseWriter, r *http.Request) {
	leaderFound := false
	client := http.Client{Timeout: 100 * time.Millisecond}

	// Trong pBFT, thường Node 1 là Primary (View 1). 
	// Tuy nhiên ta cứ loop để tìm node nào chấp nhận request (Logic generic)
	for port := 60051; port <= 60055; port++ {
		url := fmt.Sprintf("http://localhost:%d/start", port)
		resp, err := client.Post(url, "application/json", nil)
		if err == nil {
			if resp.StatusCode == 200 {
				leaderFound = true
				fmt.Printf("Request accepted by Primary at port %d\n", port)
				resp.Body.Close()
				break
			}
			resp.Body.Close()
		}
	}

	if leaderFound {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Request accepted"))
	} else {
		http.Error(w, "Request failed (No Primary found or System Busy)", 503)
	}
}

func handleResetSystem(w http.ResponseWriter, r *http.Request) {
	db.Exec("DELETE FROM blocks")
	client := http.Client{Timeout: 200 * time.Millisecond}
	
	for i, port := range []int{60051, 60052, 60053, 60054, 60055} {
		nodeID := fmt.Sprintf("node%d", i+1)
		
		// 1. Reset Internal State
		go client.Get(fmt.Sprintf("http://localhost:%d/reset", port))
		
		// 2. Set back to Honest
		go func(p int, nid string) {
			payload, _ := json.Marshal(map[string]string{"action": "honest"})
			client.Post(fmt.Sprintf("http://localhost:%d/config", p), "application/json", bytes.NewBuffer(payload))
		}(port, nodeID)

		nodeStatus[nodeID] = "honest"
	}
	
	broadcast <- Event{EventType: "RESET", Message: "System Reset & Nodes Set to Honest", Color: "blue", SourceNode: "System"}
	w.WriteHeader(http.StatusOK)
}

func handleNodeConfig(w http.ResponseWriter, r *http.Request) {
	var req struct { NodeID string `json:"node_id"`; Action string `json:"action"` }
	json.NewDecoder(r.Body).Decode(&req)
	
	portMap := map[string]int{"node1":60051, "node2":60052, "node3":60053, "node4":60054, "node5":60055}
	if port, ok := portMap[req.NodeID]; ok {
		url := fmt.Sprintf("http://localhost:%d/config", port)
		body, _ := json.Marshal(req)
		http.Post(url, "application/json", bytes.NewBuffer(body))
		
		// Update UI Status
		if req.Action == "malicious" { nodeStatus[req.NodeID] = "malicious" }
		if req.Action == "honest" { nodeStatus[req.NodeID] = "honest" }
	}
	w.WriteHeader(http.StatusOK)
}

func handleReport(w http.ResponseWriter, r *http.Request) {
	var evt Event
	json.NewDecoder(r.Body).Decode(&evt)
	if evt.EventType == "CONSENSUS" || evt.EventType == "COMMITTED" {
		// pBFT dùng COMMITTED hoặc CONSENSUS đều được, map vào đây
		if evt.Message != "" {
			var seq int
			// Thử parse format của pBFT core
			_, err := fmt.Sscanf(evt.Message, "+++ BLOCK #%d COMMITTED +++", &seq)
			if err != nil {
				// Fallback format cũ nếu có
				fmt.Sscanf(evt.Message, "+++ CONSENSUS REACHED +++ Block %d", &seq)
			}
			if seq > 0 && evt.BlockHash != "" { go saveBlock(seq, evt.BlockHash) }
		}
	}
	broadcast <- evt
	w.WriteHeader(http.StatusOK)
}


func handleGetLedger(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.Query("SELECT sequence, prev_hash, hash, timestamp FROM blocks ORDER BY sequence DESC")
	defer rows.Close()
	var blocks []BlockRecord
	for rows.Next() {
		var b BlockRecord
		rows.Scan(&b.Sequence, &b.PrevHash, &b.Hash, &b.Timestamp)
		blocks = append(blocks, b)
	}
	if blocks == nil { blocks = []BlockRecord{} }
	json.NewEncoder(w).Encode(blocks)
}

func handleAskGemini(w http.ResponseWriter, r *http.Request) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" { http.Error(w, "Missing API Key", 500); return }
	var req struct { Logs string `json:"logs"` }; json.NewDecoder(r.Body).Decode(&req)
	prompt := "Phân tích log từ hệ thống mô phỏng thuật toán đồng thuận pBFT này (PrePrepare, Prepare, Commit). Giải thích bất kỳ hành vi độc hại nào:\n" + req.Logs
	body, _ := json.Marshal(map[string]interface{}{"contents": []interface{}{map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": prompt}}}}})
	resp, _ := http.Post("https://generativelanguage.googleapis.com/v1beta/models/gemini-flash-latest:generateContent?key="+key, "application/json", bytes.NewBuffer(body))
	defer resp.Body.Close(); io.Copy(w, resp.Body)
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, _ := upgrader.Upgrade(w, r, nil); defer ws.Close()
	mu.Lock(); clients[ws] = true; mu.Unlock()
	statusPayload, _ := json.Marshal(map[string]interface{}{"type": "STATUS_SYNC", "data": nodeStatus})
	ws.WriteMessage(websocket.TextMessage, statusPayload)
	for { var msg interface{}; if ws.ReadJSON(&msg) != nil { mu.Lock(); delete(clients, ws); mu.Unlock(); break } }
}

func handleMessages() {
	for evt := range broadcast {
		mu.Lock(); for client := range clients { client.WriteJSON(evt) }; mu.Unlock()
	}
}