# /// script
# requires-python = ">=3.12"
# dependencies = [
#     "requests",
#     "colorama"
# ]
# ///

import requests
import time
import sys
from colorama import init, Fore, Style

# --- CONFIG ---
DASHBOARD_URL = "http://localhost:8080"
init(autoreset=True)

# --- UTILS ---
def log(type, msg):
    colors = {
        "INFO": Fore.BLUE,
        "PASS": Fore.GREEN,
        "FAIL": Fore.RED,
        "WARN": Fore.YELLOW,
        "THEORY": Fore.CYAN
    }
    prefix = f"[{colors.get(type, Fore.WHITE)}{type}{Style.RESET_ALL}]"
    print(f"{prefix} {msg}")

def reset_system():
    try:
        # Reset DB và đưa Node về Honest
        requests.get(f"{DASHBOARD_URL}/api/control/reset", timeout=5)
        log("INFO", "System Reset. Waiting 2s for state synchronization...")
        time.sleep(2)
        return True
    except Exception as e:
        log("FAIL", f"Reset Failed: {e}")
        return False

def set_malicious(node_id, is_malicious):
    """Giả lập Node bị chiếm quyền (Byzantine) hoặc trung thực"""
    action = "malicious" if is_malicious else "honest"
    try:
        resp = requests.post(f"{DASHBOARD_URL}/api/control/config", 
                      json={"node_id": node_id, "action": action}, timeout=1)
        if resp.status_code == 200:
            status = "😈 MALICIOUS (Byzantine)" if is_malicious else "😇 HONEST"
            print(f"   -> Configured {node_id} as {status}")
            time.sleep(0.5) # Chờ node cập nhật trạng thái
        else:
            log("WARN", f"Config failed for {node_id}")
    except:
        log("WARN", f"Could not connect to {node_id}")

def trigger_client_request():
    """Client gửi lệnh tạo Block mới"""
    try:
        resp = requests.post(f"{DASHBOARD_URL}/api/control/start", timeout=2)
        if resp.status_code == 200:
            return True
    except:
        pass
    return False

def get_chain_length():
    try:
        resp = requests.get(f"{DASHBOARD_URL}/api/ledger", timeout=2)
        data = resp.json()
        return len(data) if data else 0
    except:
        return 0
    
def get_current_view():
    """Lấy View ID hiện tại từ hệ thống để kiểm tra View Change"""
    try:
        # Gọi API /api/status mà chúng ta vừa thêm vào Dashboard
        resp = requests.get(f"{DASHBOARD_URL}/api/status", timeout=2)
        if resp.status_code == 200:
            data = resp.json()
            # Trả về field 'view' từ JSON
            return data.get("view", 0) 
    except:
        pass
    return 0

# --- TEST CASES ---

def test_01_happy_path():
    print(f"\n{Fore.MAGENTA}=== TC-01: Happy Path (All Nodes Honest) ==={Style.RESET_ALL}")
    if not reset_system(): return
    
    log("INFO", "Client sends request to Primary (Node 1 default)...")
    if trigger_client_request():
        time.sleep(2) # Chờ 3-phase commit
        count = get_chain_length()
        if count == 1:
            log("PASS", "Consensus Reached. Block #1 committed.")
        else:
            log("FAIL", f"Expected 1 block, found {count}")
    else:
        log("FAIL", "Client request rejected.")

def test_02_tolerance_1_fault():
    print(f"\n{Fore.MAGENTA}=== TC-02: Resilience Level 1 (1 Malicious) ==={Style.RESET_ALL}")
    log("THEORY", "Config: N=5, f=1. Quorum = 3f+1 = 4.")
    log("THEORY", "Scenario: 4 Honest Nodes >= 4 Required. System SHOULD WORK.")
    
    if not reset_system(): return

    set_malicious("node5", True)
    
    log("INFO", "Client sends request (with 4/5 honest nodes)...")
    trigger_client_request()
    time.sleep(2)
    
    if get_chain_length() == 1:
        log("PASS", "System survived 1 malicious node (Strong Quorum met).")
    else:
        log("FAIL", "System failed unexpectedly.")

def test_03_tolerance_2_faults():
    print(f"\n{Fore.MAGENTA}=== TC-03: Resilience Level 2 (2 Malicious) ==={Style.RESET_ALL}")
    log("THEORY", "Config: N=5. Standard Quorum=3.")
    log("THEORY", "Scenario: 2 Malicious -> 3 Honest left.")
    log("THEORY", "Analysis: 3 Honest >= Quorum 3. System SHOULD SURVIVE (Benign Case).")
    
    reset_system()
    set_malicious("node4", True)
    set_malicious("node5", True) # 2 Node chết
    
    trigger_client_request(); time.sleep(2)
    
    if get_chain_length() == 1: 
        log("PASS", "System survived 2 faults! (Redundancy Advantage of N=5).")
    else: 
        log("FAIL", "System failed but theoretically should pass with N=5.")

def test_04_chain_integrity():
    print(f"\n{Fore.MAGENTA}=== TC-04: Blockchain Integrity & Continuity ==={Style.RESET_ALL}")
    if not reset_system(): return
    
    log("INFO", "Sending 3 consecutive requests...")
    for i in range(3):
        trigger_client_request()
        time.sleep(1)
        print(f"   -> Request {i+1} sent")
    
    time.sleep(1)
    count = get_chain_length()
    
    if count == 3:
        log("PASS", "Chain grew to 3 blocks correctly.")
    else:
        log("FAIL", f"Integrity check failed. Expected 3, found {count}.")

def test_05_primary_failover():
    print(f"\n{Fore.MAGENTA}=== TC-05: Primary Node Malicious (View Change & Retry) ==={Style.RESET_ALL}")
    log("THEORY", "Scenario: Node 1 Malicious. System cycles Views (1->2).")
    log("THEORY", "Mechanism: Timeout (10s) -> View Change -> Client Retry -> Node 2 Commits.")
    
    if not reset_system(): return

    # 1. Kiểm tra trạng thái ban đầu
    initial_view = get_current_view() 
    initial_height = get_chain_length()
    log("INFO", f"Initial State: View={initial_view}, Height={initial_height}")

    # 2. Tấn công: Biến Node 1 thành Malicious
    log("INFO", "Setting Node 1 (Default Primary) as 😈 MALICIOUS...")
    set_malicious("node1", True)
    
    # 3. Chờ Timeout kích hoạt View Change
    # Trong code pbft.go, BaseTimeout = 10s. Chúng ta cần chờ lâu hơn thế.
    log("INFO", "Waiting for Primary Timeout (approx 12-15s)...")
    
    new_view_established = False
    current_view = initial_view
    
    # Vòng lặp chờ View thay đổi (Chờ tối đa 20s)
    for i in range(20):
        time.sleep(1)
        current_view = get_current_view()
        
        # Nếu View đã tăng (tức là Node 1 đã bị phế truất)
        if current_view > initial_view:
            log("PASS", f"View Change detected! T+{i+1}s: View moved from {initial_view} to {current_view}.")
            new_view_established = True
            break
        else:
             print(f"   -> T+{i+1}s: Still View {current_view} (Waiting for 10s timeout)...", end='\r')
    print("") # Xuống dòng cho đẹp

    if not new_view_established:
        log("FAIL", "System did not trigger View Change within timeout limit.")
        set_malicious("node1", False)
        return

    # 4. Gửi lại Request (Client Retry Logic)
    # Vì request đầu tiên gửi cho Node 1 đã bị drop, Client phải gửi lại cho mạng.
    # Dashboard sẽ tự động tìm Primary mới (Node 2) để gửi vào.
    log("INFO", "View changed. Client RETRIES request to new Primary...")
    if trigger_client_request():
        log("INFO", "Request sent. Waiting for Consensus...")
    else:
        log("FAIL", "New Primary rejected request.")
        set_malicious("node1", False)
        return
    
    # 5. Chờ Block Commit
    success = False
    for i in range(5):
        time.sleep(1)
        current_height = get_chain_length()
        if current_height > initial_height:
            success = True
            break
            
    # 6. Kiểm tra kết quả
    if success:
        log("PASS", f"Failover Successful! Block committed at Height {current_height} in View {current_view}.")
    else:
        log("FAIL", "View changed but Block was NOT committed.")

    # Cleanup: Trả lại trạng thái Honest cho Node 1
    set_malicious("node1", False)

# --- RUNNER ---
if __name__ == "__main__":
    print(f"{Style.BRIGHT}{Fore.CYAN}pBFT TESTING...{Style.RESET_ALL}")
    print(f"Target: {DASHBOARD_URL}")
    print("-" * 60)
    
    try:
        requests.get(DASHBOARD_URL)
    except:
        log("FAIL", "Dashboard is OFFLINE. Start ./run_network.sh first.")
        sys.exit(1)

    test_01_happy_path()
    test_02_tolerance_1_fault()
    test_03_tolerance_2_faults()
    test_04_chain_integrity()
    test_05_primary_failover()
    
    print("-" * 60)
    print(f"{Style.BRIGHT}TEST SUITE COMPLETED.{Style.RESET_ALL}")