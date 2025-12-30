import subprocess, time, grpc, random, os, sys

# Thêm đường dẫn để python tìm thấy file proto đã biên dịch
sys.path.append(os.path.dirname(os.path.abspath(__file__)))
import consensus_pb2 as raft_pb2
import consensus_pb2_grpc as raft_pb2_grpc

class RaftTester:
    def __init__(self):
        self.nodes = {}
        self.ports = ["50050", "50051", "50052", "50053", "50054"]
        self.exe = "../raft_node.exe"

    def start_node(self, i):
        # Chạy node trong thư mục Raft/
        self.nodes[i] = subprocess.Popen([self.exe, "-id", str(i)], cwd="../", stdout=subprocess.DEVNULL)

    def stop_node(self, i):
        if i in self.nodes:
            self.nodes[i].terminate()
            del self.nodes[i]

    def get_leader(self):
        for i in range(5):
            try:
                channel = grpc.insecure_channel(f'localhost:{self.ports[i]}')
                resp = raft_pb2_grpc.RaftServiceStub(channel).GetStatus(raft_pb2.Empty(), timeout=0.2)
                if resp.state == "Leader": return resp.id
            except: pass
        return None

    def run(self):
        # ... logic test giữ nguyên ...
        pass