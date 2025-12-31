package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	// Import từ module chung
	pb "consensus/common/proto" 
)

// --- CONFIG ---
const (
	DashboardURL = "http://localhost:8080/api/report"
	TotalNodes   = 5
	Faults       = 1
	
	
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
	Peers       map[string]string
	PeerClients map[string]pb.ConsensusServiceClient

	View           int64
	Sequence       int64
	Blockchain     []Block
	IsMalicious    bool

	// [FIX] Sửa lại kiểu dữ liệu map thành PbftMessage
	PrepareMsgs map[int64]map[string]*pb.PbftMessage
	CommitMsgs  map[int64]map[string]*pb.PbftMessage
	Committed   map[int64]bool
}

func NewServer(id string, peers map[string]string) *Server {
	return &Server{
		NodeID:      id,
		Peers:       peers,
		PeerClients: make(map[string]pb.ConsensusServiceClient),
		View:        1,
		Sequence:    0,
		Blockchain:  []Block{{0, "0000", "Genesis-Hash", "Genesis"}},
		IsMalicious: false,
		// [FIX] Khởi tạo map với PbftMessage
		PrepareMsgs: make(map[int64]map[string]*pb.PbftMessage),
		CommitMsgs:  make(map[int64]map[string]*pb.PbftMessage),
		Committed:   make(map[int64]bool),
	}
}

// --- PHASE 1: PRE-PREPARE ---
func (s *Server) StartConsensus() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsMalicious {
		s.report("MALICIOUS", "Attempted to start consensus but blocked", "red")
		return fmt.Errorf("malicious node cannot start consensus")
	}

	newSeq := s.Sequence + 1
	prevBlock := s.Blockchain[len(s.Blockchain)-1]
	
	data := fmt.Sprintf("Block #%d Data", newSeq)
	hashInput := fmt.Sprintf("%d%s%s%d", newSeq, prevBlock.Hash, data, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(hashInput))
	blockHash := hex.EncodeToString(hash[:])

	// [FIX] Dùng PbftMessage và đúng tên trường (View, Sequence)
	msg := &pb.PbftMessage{
		Type:          "PrePrepare",
		NodeId:        s.NodeID,
		View:          s.View,      // Proto dùng View
		Sequence:      newSeq,      // Proto dùng Sequence
		BlockHash:     blockHash,
		PrevBlockHash: prevBlock.Hash,
		Data:          data,
		Timestamp:     time.Now().UnixMilli(),
	}

	s.report("START", fmt.Sprintf("Primary proposed Block #%d", newSeq), "blue")
	go s.Broadcast(msg)
	return nil
}

// --- RPC HANDLE (Cập nhật tên hàm theo Proto) ---


func (s *Server) HandlePbftMessage(ctx context.Context, req *pb.PbftMessage) (*pb.PbftResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsMalicious { return &pb.PbftResponse{Success: false}, nil }

	// [FIX] Proto dùng req.View
	if req.View != s.View {
		return &pb.PbftResponse{Success: false}, nil
	}

	switch req.Type {
	case "PrePrepare": s.handlePrePrepare(req)
	case "Prepare":    s.handlePrepare(req)
	case "Commit":     s.handleCommit(req)
	}
	return &pb.PbftResponse{Success: true}, nil
}

// --- LOGIC 3 PHA ---

func (s *Server) handlePrePrepare(req *pb.PbftMessage) {
	if req.Sequence <= s.Sequence { return }
	
	s.report("PRE-PREPARE", fmt.Sprintf("Accepted Block #%d", req.Sequence), "cyan")

	// [FIX] Tạo PbftMessage
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
		if msg.BlockHash == req.BlockHash { count++ }
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
		if msg.BlockHash == req.BlockHash { count++ }
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
	}
}

// --- UTILS ---

// [FIX] Input là PbftMessage
func (s *Server) Broadcast(msg *pb.PbftMessage) {
	for _, client := range s.PeerClients {
		go func(c pb.ConsensusServiceClient) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			
			// [FIX] Gọi RPC HandlePbftMessage
			c.HandlePbftMessage(ctx, msg) 
		}(client)
	}
	// Loopback
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
	s.mu.Lock(); defer s.mu.Unlock()
	s.IsMalicious = malicious
	if malicious { s.report("CONFIG", "Became MALICIOUS (Byzantine)", "red") } else { s.report("CONFIG", "Became HONEST", "blue") }
}

func (s *Server) Reset() {
	s.mu.Lock(); defer s.mu.Unlock()
	s.Sequence = 0; s.Blockchain = []Block{{0, "0000", "Genesis-Hash", "Genesis"}}
	s.PrepareMsgs = make(map[int64]map[string]*pb.PbftMessage)
	s.CommitMsgs = make(map[int64]map[string]*pb.PbftMessage)
	s.Committed = make(map[int64]bool); s.IsMalicious = false
	s.report("RESET", "System Reset", "blue")
}

func (s *Server) report(evt, msg, color string) {
	go func() {
		payload := map[string]interface{}{"source_node": s.NodeID, "event_type": evt, "message": msg, "color": color, "timestamp": time.Now().UnixMilli()}
		jsonVal, _ := json.Marshal(payload); http.Post(DashboardURL, "application/json", bytes.NewBuffer(jsonVal))
	}()
}

func (s *Server) sendCommitToDB(b Block) {
	go func() {
		payload := map[string]interface{}{
			"source_node": s.NodeID, "event_type": "CONSENSUS", 
			"message": fmt.Sprintf("+++ BLOCK #%d COMMITTED +++", b.Sequence),
			"block_hash": b.Hash,
			"prev_hash": b.PrevHash,
		}
		jsonVal, _ := json.Marshal(payload)
		http.Post(DashboardURL, "application/json", bytes.NewBuffer(jsonVal))
	}()
}