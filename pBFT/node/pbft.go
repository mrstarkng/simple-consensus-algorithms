package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	// Import từ module chung
	pb "consensus/common/proto"
)

// --- CONFIG ---
const (
	DashboardURL      = "http://localhost:8080/api/report"
	TotalNodes        = 5
	Faults            = 1
	BaseTimeout       = 5 * time.Second 
	
	// Quorum chuẩn pBFT: 2f + 1
	Quorum = 2*Faults + 1
)

// --- STRUCTURES ---
type Block struct {
	Sequence int64
	PrevHash string
	Hash     string
	Data     string
}

type Server struct {
	pb.UnimplementedConsensusServiceServer
	mu sync.Mutex

	NodeID      string
	NodeIndex   int 
	Peers       map[string]string
	PeerClients map[string]pb.ConsensusServiceClient

	View           int64
	Sequence       int64
	Blockchain     []Block
	IsMalicious    bool

	// Message Logs
	PrepareMsgs map[int64]map[string]*pb.PbftMessage
	CommitMsgs  map[int64]map[string]*pb.PbftMessage
	Committed   map[int64]bool

	// View Change State
	ViewChangeMsgs map[int64]map[string]bool 
	LastActive     time.Time                 
	Timer          *time.Timer
	CurrentTimeout time.Duration // [FIX] Để xử lý Backoff
}

func NewServer(id string, peers map[string]string) *Server {
	idx, _ := strconv.Atoi(id[len(id)-1:])

	s := &Server{
		NodeID:      id,
		NodeIndex:   idx, 
		Peers:       peers,
		PeerClients: make(map[string]pb.ConsensusServiceClient),
		View:        1, 
		Sequence:    0,
		Blockchain:  []Block{{0, "0000", "Genesis-Hash", "Genesis"}},
		IsMalicious: false,

		PrepareMsgs:    make(map[int64]map[string]*pb.PbftMessage),
		CommitMsgs:     make(map[int64]map[string]*pb.PbftMessage),
		Committed:      make(map[int64]bool),
		ViewChangeMsgs: make(map[int64]map[string]bool),
		LastActive:     time.Now(),
		CurrentTimeout: BaseTimeout, // Khởi tạo timeout
	}

	s.startTimer()
	s.report("INIT", "Node started (Honest)", "gray")
	return s
}

// Timer Loop với Backoff
// Timer Loop với Backoff
func (s *Server) startTimer() {
    if s.Timer != nil {
        s.Timer.Stop()
    }
    
    s.Timer = time.AfterFunc(s.CurrentTimeout, func() {
        s.mu.Lock()
        defer s.mu.Unlock()

        if s.IsMalicious {
            s.resetTimer() 
            return
        }

        // Kiểm tra xem đã quá hạn chưa
        if time.Since(s.LastActive) > s.CurrentTimeout {
            s.report("TIMEOUT", fmt.Sprintf("Primary inactive (%v). Starting ViewChange to %d", s.CurrentTimeout, s.View+1), "orange")
            
            // Exponential Backoff: Tăng thời gian chờ
            s.CurrentTimeout *= 2
            if s.CurrentTimeout > 60*time.Second {
                s.CurrentTimeout = 60 * time.Second 
            }
            
            // 1. Kích hoạt bầu cử View mới
            s.startViewChange()

            // [FIX QUAN TRỌNG]
            // Dù đã start ViewChange, vẫn phải restart timer!
            // Lý do: Nếu Primary của View mới (VD: Node 1 ở View 6) cũng là Malicious và im lặng,
            // chúng ta cần timeout tiếp để nhảy sang View 7 (Node 2).
            // Reset LastActive giả định để timer có mốc tính mới.
            s.LastActive = time.Now() 
            s.startTimer() 

        } else {
            // Nếu chưa timeout thật sự thì reset
            s.startTimer()
        }
    })
}

func (s *Server) resetTimer() {
	if s.Timer != nil {
		s.Timer.Stop()
	}
	s.LastActive = time.Now()
	s.startTimer()
}

// --- PHASE 1: PRE-PREPARE ---
func (s *Server) StartConsensus() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// [FIX] Chặn chặt chẽ hơn: Phải đúng View mới được làm Primary
	expectedPrimaryIdx := int(s.View-1)%TotalNodes + 1
	if s.NodeIndex != expectedPrimaryIdx {
		return fmt.Errorf("node%d is NOT Primary for View %d (Primary is node%d). Cannot start.", s.NodeIndex, s.View, expectedPrimaryIdx)
	}

	if s.IsMalicious {
		s.report("MALICIOUS", "Primary blocked consensus start", "red")
		return fmt.Errorf("malicious node blocked")
	}

	newSeq := s.Sequence + 1
	prevBlock := s.Blockchain[len(s.Blockchain)-1]
	
	data := fmt.Sprintf("Block #%d Data", newSeq)
	hashInput := fmt.Sprintf("%d%s%s%d", newSeq, prevBlock.Hash, data, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(hashInput))
	blockHash := hex.EncodeToString(hash[:])

	msg := &pb.PbftMessage{
		Type:          "PrePrepare",
		NodeId:        s.NodeID,
		View:          s.View,
		Sequence:      newSeq,
		BlockHash:     blockHash,
		PrevBlockHash: prevBlock.Hash,
		Data:          data,
		Timestamp:     time.Now().UnixMilli(),
	}

	s.report("START", fmt.Sprintf("Primary proposed Block #%d", newSeq), "blue")
	go s.Broadcast(msg)
	return nil
}

// --- RPC HANDLE ---
func (s *Server) HandlePbftMessage(ctx context.Context, req *pb.PbftMessage) (*pb.PbftResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsMalicious {
		return &pb.PbftResponse{Success: false}, nil
	}

	// [FIX] Nếu nhận được tin nhắn từ View cao hơn hẳn -> Cập nhật ngay (Fast Catchup)
	// Điều này giúp node bị lag (do malicious cũ) bắt kịp mạng lưới
	if req.View > s.View {
		s.View = req.View
		s.CurrentTimeout = BaseTimeout // Reset backoff khi bắt kịp
		s.report("SYNC", fmt.Sprintf("Caught up to View %d via message from %s", s.View, req.NodeId), "purple")
	}

	// Logic xử lý tin nhắn
	s.LastActive = time.Now() // Reset activity

	switch req.Type {
	case "PrePrepare":
		if req.View == s.View { s.handlePrePrepare(req) }
	case "Prepare":
		if req.View == s.View { s.handlePrepare(req) }
	case "Commit":
		if req.View == s.View { s.handleCommit(req) }
	case "ViewChange":
		s.handleViewChange(req)
	case "NewView":
		s.handleNewView(req)
	}

	return &pb.PbftResponse{Success: true}, nil
}

// --- LOGIC VIEW CHANGE (Improved) ---

func (s *Server) startViewChange() {
	nextView := s.View + 1
	
	msg := &pb.PbftMessage{
		Type:      "ViewChange",
		NodeId:    s.NodeID,
		View:      nextView, 
		Timestamp: time.Now().UnixMilli(),
	}
	
	if _, ok := s.ViewChangeMsgs[nextView]; !ok {
		s.ViewChangeMsgs[nextView] = make(map[string]bool)
	}
	s.ViewChangeMsgs[nextView][s.NodeID] = true
	
	go s.Broadcast(msg)
}

func (s *Server) handleViewChange(req *pb.PbftMessage) {
	newView := req.View
	if newView <= s.View { return } // Bỏ qua view cũ

	if _, ok := s.ViewChangeMsgs[newView]; !ok {
		s.ViewChangeMsgs[newView] = make(map[string]bool)
	}
	s.ViewChangeMsgs[newView][req.NodeId] = true

	count := len(s.ViewChangeMsgs[newView])
	
	// Tính Primary cho View mới
	expectedPrimaryIdx := int(newView-1)%TotalNodes + 1
	
	if s.NodeIndex == expectedPrimaryIdx {
		if count >= Quorum {
			s.report("LEADER-ELECTION", fmt.Sprintf("I am new Primary for View %d! (Votes: %d)", newView, count), "purple")
			
			newViewMsg := &pb.PbftMessage{
				Type:      "NewView",
				NodeId:    s.NodeID,
				View:      newView,
				Timestamp: time.Now().UnixMilli(),
			}
			// Tự xử lý NewView cho chính mình trước để reset state
			s.processNewView(newView)
			go s.Broadcast(newViewMsg)
		}
	}
}

func (s *Server) handleNewView(req *pb.PbftMessage) {
	if req.View > s.View {
		s.processNewView(req.View)
		s.report("NEW-VIEW", fmt.Sprintf("Followed new Primary %s to View %d", req.NodeId, s.View), "purple")
	}
}

// [FIX] Hàm logic chung để chuyển View an toàn
func (s *Server) processNewView(newView int64) {
	s.View = newView
	s.CurrentTimeout = BaseTimeout // Reset backoff về mặc định
	
	// [QUAN TRỌNG] Clear state cũ để tránh "rác"
	// Chỉ giữ lại blockchain, xóa phiếu bầu của các view cũ
	s.ViewChangeMsgs = make(map[int64]map[string]bool)
	
	// Restart timer để chờ Block mới
	s.resetTimer()
}

// --- LOGIC 3 PHA (Giữ nguyên) ---

func (s *Server) handlePrePrepare(req *pb.PbftMessage) {
	if req.Sequence <= s.Sequence { return }

	s.report("PRE-PREPARE", fmt.Sprintf("Accepted Block #%d from %s", req.Sequence, req.NodeId), "cyan")

	prepareMsg := &pb.PbftMessage{
		Type:          "Prepare",
		NodeId:        s.NodeID,
		View:          req.View,
		Sequence:      req.Sequence,
		BlockHash:     req.BlockHash,
		PrevBlockHash: req.PrevBlockHash,
	}
	go s.Broadcast(prepareMsg)
}

func (s *Server) handlePrepare(req *pb.PbftMessage) {
	seq := req.Sequence
	if _, ok := s.PrepareMsgs[seq]; !ok {
		s.PrepareMsgs[seq] = make(map[string]*pb.PbftMessage)
	}

	s.PrepareMsgs[seq][req.NodeId] = req

	count := 0
	for _, msg := range s.PrepareMsgs[seq] {
		if msg.BlockHash == req.BlockHash {
			count++
		}
	}

	if count >= Quorum {
		commitMsg := &pb.PbftMessage{
			Type:          "Commit",
			NodeId:        s.NodeID,
			View:          req.View,
			Sequence:      req.Sequence,
			BlockHash:     req.BlockHash,
			PrevBlockHash: req.PrevBlockHash,
			Data:          req.Data,
		}

		if !s.Committed[seq] {
			go s.Broadcast(commitMsg)
		}
	}
}

func (s *Server) handleCommit(req *pb.PbftMessage) {
	seq := req.Sequence
	if s.Committed[seq] { return }

	if _, ok := s.CommitMsgs[seq]; !ok {
		s.CommitMsgs[seq] = make(map[string]*pb.PbftMessage)
	}

	s.CommitMsgs[seq][req.NodeId] = req

	count := 0
	for _, msg := range s.CommitMsgs[seq] {
		if msg.BlockHash == req.BlockHash {
			count++
		}
	}

	if count >= Quorum {
		s.Committed[seq] = true
		s.Sequence = seq

		newBlock := Block{
			Sequence: seq,
			PrevHash: req.PrevBlockHash,
			Hash:     req.BlockHash,
			Data:     fmt.Sprintf("Block #%d", seq),
		}
		s.Blockchain = append(s.Blockchain, newBlock)

		s.report("COMMITTED", fmt.Sprintf("+++ BLOCK #%d COMMITTED +++", seq), "green")
		s.sendCommitToDB(newBlock)
		
		// [FIX] Reset timer sau khi commit thành công để tránh timeout oan
		s.resetTimer()
	}
}

// --- UTILS ---

func (s *Server) Broadcast(msg *pb.PbftMessage) {
	for _, client := range s.PeerClients {
		go func(c pb.ConsensusServiceClient) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			c.HandlePbftMessage(ctx, msg)
		}(client)
	}
	go func() {
		time.Sleep(5 * time.Millisecond)
		s.HandlePbftMessage(context.Background(), msg)
	}()
}

func (s *Server) ConnectToPeers() {
	time.Sleep(1 * time.Second)
	for peerID, addr := range s.Peers {
		conn, _ := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		s.PeerClients[peerID] = pb.NewConsensusServiceClient(conn)
	}
}

func (s *Server) SetMalicious(malicious bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	// [FIX] Khi chuyển từ Malicious -> Honest
	if s.IsMalicious && !malicious {
		s.report("CONFIG", "Became HONEST - Syncing state...", "blue")
		s.CurrentTimeout = BaseTimeout // Reset backoff
		s.resetTimer() // Kích hoạt lại timer ngay lập tức
	}
	
	s.IsMalicious = malicious
	if malicious {
		s.report("CONFIG", "Became MALICIOUS (Byzantine)", "red")
	}
}

func (s *Server) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.View = 1
	s.Sequence = 0
	s.Blockchain = []Block{{0, "0000", "Genesis-Hash", "Genesis"}}
	s.PrepareMsgs = make(map[int64]map[string]*pb.PbftMessage)
	s.CommitMsgs = make(map[int64]map[string]*pb.PbftMessage)
	s.ViewChangeMsgs = make(map[int64]map[string]bool)
	s.Committed = make(map[int64]bool)
	s.IsMalicious = false
	s.CurrentTimeout = BaseTimeout
	s.resetTimer()
	s.report("RESET", "System Reset to View 1", "blue")
}

func (s *Server) report(evt, msg, color string) {
	go func() {
		payload := map[string]interface{}{"source_node": s.NodeID, "event_type": evt, "message": msg, "color": color, "timestamp": time.Now().UnixMilli(),}
		jsonVal, _ := json.Marshal(payload)
		http.Post(DashboardURL, "application/json", bytes.NewBuffer(jsonVal))
	}()
}

func (s *Server) sendCommitToDB(b Block) {
	go func() {
		payload := map[string]interface{}{
			"source_node": s.NodeID, "event_type": "CONSENSUS",
			"message":    fmt.Sprintf("+++ BLOCK #%d COMMITTED +++", b.Sequence),
			"block_hash": b.Hash,
			"prev_hash":  b.PrevHash,
		}
		jsonVal, _ := json.Marshal(payload)
		http.Post(DashboardURL, "application/json", bytes.NewBuffer(jsonVal))
	}()
}