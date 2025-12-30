package main

import (
	"consensus/common/proto"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type NodeState int

const (
	Follower NodeState = iota
	Candidate
	Leader
)

type RaftNode struct {
	proto.UnimplementedConsensusServiceServer
	mu            sync.Mutex
	me            int32
	peers         map[int32]string
	state         NodeState
	currentTerm   int64
	votedFor      int32
	logs          []*proto.LogEntry
	blacklist     map[int32]bool
	electionTimer *time.Timer
}

func NewRaftNode(id int32, peers map[int32]string) *RaftNode {
	rn := &RaftNode{
		me:        id,
		peers:     peers,
		state:     Follower,
		votedFor:  -1,
		blacklist: make(map[int32]bool),
	}
	rn.load()
	rn.resetElectionTimer()
	return rn
}

func (rn *RaftNode) save() {
	_ = os.MkdirAll("logs", 0755) // Lưu vào folder logs nội bộ của Raft
	data, _ := json.Marshal(rn.logs)
	filename := fmt.Sprintf("logs/storage_%d.json", rn.me)
	_ = os.WriteFile(filename, data, 0644)
}

func (rn *RaftNode) load() {
	filename := fmt.Sprintf("logs/storage_%d.json", rn.me)
	data, err := os.ReadFile(filename)
	if err == nil {
		_ = json.Unmarshal(data, &rn.logs)
	}
}

func (rn *RaftNode) resetElectionTimer() {
	if rn.electionTimer != nil {
		rn.electionTimer.Stop()
	}
	timeout := time.Duration(400+rand.Intn(400)) * time.Millisecond
	rn.electionTimer = time.AfterFunc(timeout, rn.startElection)
}

func (rn *RaftNode) RequestVote(ctx context.Context, args *proto.RequestVoteArgs) (*proto.RequestVoteReply, error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if rn.blacklist[args.CandidateId] {
		return nil, fmt.Errorf("Partition")
	}
	if args.Term > rn.currentTerm {
		rn.currentTerm, rn.state, rn.votedFor = args.Term, Follower, -1
	}
	reply := &proto.RequestVoteReply{Term: rn.currentTerm, VoteGranted: false}
	if (rn.votedFor == -1 || rn.votedFor == args.CandidateId) && args.Term >= rn.currentTerm {
		rn.votedFor = args.CandidateId
		reply.VoteGranted = true
		rn.resetElectionTimer()
	}
	return reply, nil
}

func (rn *RaftNode) AppendEntries(ctx context.Context, args *proto.AppendEntriesArgs) (*proto.AppendEntriesReply, error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if rn.blacklist[args.LeaderId] {
		return nil, fmt.Errorf("Partition")
	}
	if args.Term >= rn.currentTerm {
		rn.state, rn.currentTerm = Follower, args.Term
		rn.resetElectionTimer()
		if len(args.Entries) > len(rn.logs) {
			rn.logs = args.Entries
			rn.save()
		}
		return &proto.AppendEntriesReply{Term: rn.currentTerm, Success: true}, nil
	}
	return &proto.AppendEntriesReply{Term: rn.currentTerm, Success: false}, nil
}

func (rn *RaftNode) Propose(ctx context.Context, args *proto.ProposeArgs) (*proto.ProposeReply, error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if rn.state != Leader {
		return &proto.ProposeReply{Success: false}, nil
	}
	rn.logs = append(rn.logs, &proto.LogEntry{Term: rn.currentTerm, Index: int64(len(rn.logs)), Command: args.Command})
	rn.save()
	return &proto.ProposeReply{Success: true, LeaderId: rn.me}, nil
}

func (rn *RaftNode) GetStatus(ctx context.Context, _ *proto.Empty) (*proto.StatusReply, error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	states := []string{"Follower", "Candidate", "Leader"}
	return &proto.StatusReply{Id: rn.me, State: states[rn.state], Term: rn.currentTerm}, nil
}

func (rn *RaftNode) ForceLeader(ctx context.Context, _ *proto.Empty) (*proto.Empty, error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.currentTerm += 100
	rn.state = Leader
	go rn.becomeLeader()
	return &proto.Empty{}, nil
}

func (rn *RaftNode) SetNetworkPartition(ctx context.Context, args *proto.PartitionArgs) (*proto.PartitionReply, error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.blacklist = make(map[int32]bool)
	for _, id := range args.IsolatedNodeIds {
		rn.blacklist[id] = true
	}
	return &proto.PartitionReply{Success: true}, nil
}

func (rn *RaftNode) startElection() {
	rn.mu.Lock()
	if rn.state == Leader {
		rn.mu.Unlock()
		return
	}
	rn.state, rn.currentTerm, rn.votedFor = Candidate, rn.currentTerm+1, rn.me
	term := rn.currentTerm
	rn.mu.Unlock()
	votes := 1
	var once sync.Once
	for id, addr := range rn.peers {
		if id == rn.me || rn.blacklist[id] {
			continue
		}
		go func(address string) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			conn, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
			if err != nil {
				return
			}
			defer conn.Close()
			resp, err := proto.NewConsensusServiceClient(conn).RequestVote(ctx, &proto.RequestVoteArgs{Term: term, CandidateId: rn.me})
			if err == nil && resp.VoteGranted {
				rn.mu.Lock()
				votes++
				if votes >= 3 && rn.state == Candidate {
					once.Do(func() { rn.becomeLeader() })
				}
				rn.mu.Unlock()
			}
		}(addr)
	}
	rn.resetElectionTimer()
}

func (rn *RaftNode) becomeLeader() {
	rn.state = Leader
	go func() {
		for {
			rn.mu.Lock()
			if rn.state != Leader {
				rn.mu.Unlock()
				return
			}
			term, logs := rn.currentTerm, rn.logs
			rn.mu.Unlock()
			successCount := 1
			var wg sync.WaitGroup
			var mu sync.Mutex
			for id, addr := range rn.peers {
				if id == rn.me || rn.blacklist[id] {
					continue
				}
				wg.Add(1)
				go func(ad string) {
					defer wg.Done()
					ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
					defer cancel()
					conn, err := grpc.DialContext(ctx, ad, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
					if err != nil {
						return
					}
					defer conn.Close()
					resp, err := proto.NewConsensusServiceClient(conn).AppendEntries(ctx, &proto.AppendEntriesArgs{Term: term, LeaderId: rn.me, Entries: logs})
					if err == nil && resp.Success {
						mu.Lock()
						successCount++
						mu.Unlock()
					}
				}(addr)
			}
			wg.Wait()
			rn.mu.Lock()
			if successCount < 3 {
				rn.state, rn.votedFor = Follower, -1
				rn.resetElectionTimer()
				rn.mu.Unlock()
				return
			}
			rn.mu.Unlock()
			time.Sleep(150 * time.Millisecond)
		}
	}()
}

func main() {
	id := flag.Int("id", 0, "node id")
	flag.Parse()
	rand.Seed(time.Now().UnixNano() + int64(*id))
	ports := []string{"50050", "50051", "50052", "50053", "50054"}
	peers := make(map[int32]string)
	for i, p := range ports {
		peers[int32(i)] = "localhost:" + p
	}
	lis, _ := net.Listen("tcp", "localhost:"+ports[*id])
	s := grpc.NewServer()
	proto.RegisterConsensusServiceServer(s, NewRaftNode(int32(*id), peers))
	log.Printf("Node %d starting...", *id)
	s.Serve(lis)
}
