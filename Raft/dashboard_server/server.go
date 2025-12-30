package main

import (
	"consensus/common/proto"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	processes = make(map[int]*exec.Cmd)
	mu        sync.Mutex
	ports     = []string{"50050", "50051", "50052", "50053", "50054"}
)

func main() {
	// Nếu bạn chạy server từ thư mục gốc dự án (BACKUP-CONSENSUS)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "Raft/dashboard/index.html") // Đường dẫn từ gốc
	})
	http.HandleFunc("/wallpaper.jpg", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "Raft/dashboard/wallpaper.jpg")
	})

	http.HandleFunc("/api/status", getStatus)
	http.HandleFunc("/api/toggle", toggleNode)
	http.HandleFunc("/api/start_all", startAll)
	http.HandleFunc("/api/partition", partition)
	http.HandleFunc("/api/reset", resetAll)

	log.Println("Dashboard: http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}

func getStatus(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	var res []interface{}
	for i, p := range ports {
		info := map[string]interface{}{"id": i, "state": "Offline", "term": 0}
		conn, err := grpc.Dial("localhost:"+p, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithTimeout(100*time.Millisecond))
		if err == nil {
			c := proto.NewRaftServiceClient(conn)
			s, err := c.GetStatus(context.Background(), &proto.Empty{})
			if err == nil {
				info["state"], info["term"] = s.State, s.Term
			}
			conn.Close()
		}
		res = append(res, info)
	}
	json.NewEncoder(w).Encode(res)
}

func toggleNode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	action := r.URL.Query().Get("action")
	mu.Lock()
	defer mu.Unlock()
	if action == "off" {
		if c, ok := processes[id]; ok {
			c.Process.Kill()
			delete(processes, id)
		}
	} else {
		// Chạy file exe nằm trong folder Raft
		cmd := exec.Command("./Raft/raft_node.exe", "-id", strconv.Itoa(id))
		cmd.Dir = "Raft" // Chạy trong bối cảnh thư mục Raft để logs nằm đúng chỗ
		cmd.Start()
		processes[id] = cmd
	}
}

func startAll(w http.ResponseWriter, r *http.Request) {
	leader := r.URL.Query().Get("leader")
	mu.Lock()
	for i := 0; i < 5; i++ {
		if _, running := processes[i]; !running {
			cmd := exec.Command("./raft_node.exe", "-id", strconv.Itoa(i))
			cmd.Dir = "Raft" // Đảm bảo Dir là Raft
			cmd.Start()
			processes[i] = cmd
		}
	}
	mu.Unlock()
	if leader != "" {
		go func() {
			time.Sleep(1500 * time.Millisecond)
			id, _ := strconv.Atoi(leader)
			conn, _ := grpc.Dial("localhost:"+ports[id], grpc.WithTransportCredentials(insecure.NewCredentials()))
			proto.NewRaftServiceClient(conn).ForceLeader(context.Background(), &proto.Empty{})
		}()
	}
}

func resetAll(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	// Tắt sạch sẽ trên Windows
	exec.Command("taskkill", "/F", "/IM", "raft_node.exe", "/T").Run()
	for k := range processes {
		delete(processes, k)
	}
}
func partition(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("action")
	for i, p := range ports {
		list := []int32{}
		if action == "split" {
			if i < 2 {
				list = []int32{2, 3, 4}
			} else {
				list = []int32{0, 1}
			}
		}
		conn, _ := grpc.Dial("localhost:"+p, grpc.WithTransportCredentials(insecure.NewCredentials()))
		proto.NewRaftServiceClient(conn).SetNetworkPartition(context.Background(), &proto.PartitionArgs{IsolatedNodeIds: list})
	}
}
