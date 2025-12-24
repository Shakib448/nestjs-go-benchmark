# #!/bin/bash
# # WRK Battle Test - NestJS vs Go Performance Comparison
# # Usage: ./battle.sh [test_type] [duration]
# # Examples:
# #   ./battle.sh           # Run all tests with default 30s duration
# #   ./battle.sh quick     # Quick 10s test
# #   ./battle.sh full 60   # Full test with 60s duration
#
# set -e
#
# # Get script directory
# SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# cd "$SCRIPT_DIR"
#
# # ==================== CONFIGURATION ====================
# NESTJS_URL="http://localhost:3000"
# NESTJS_NAME="NestJS"
#
# GO_URL="http://localhost:3001"
# GO_NAME="Go/Fiber"
#
# # Test parameters
# DURATION="${2:-30}s"
# THREADS=12
# CONNECTIONS=200
#
# # Colors
# RED='\033[0;31m'
# GREEN='\033[0;32m'
# YELLOW='\033[1;33m'
# BLUE='\033[0;34m'
# CYAN='\033[0;36m'
# MAGENTA='\033[0;35m'
# BOLD='\033[1m'
# NC='\033[0m'
#
# # Results storage
# NESTJS_GET_RPS=0
# NESTJS_GET_LAT=0
# NESTJS_POST_RPS=0
# NESTJS_POST_LAT=0
# NESTJS_MIXED_RPS=0
# NESTJS_MIXED_LAT=0
#
# GO_GET_RPS=0
# GO_GET_LAT=0
# GO_POST_RPS=0
# GO_POST_LAT=0
# GO_MIXED_RPS=0
# GO_MIXED_LAT=0
#
# # ==================== HELPER FUNCTIONS ====================
# print_banner() {
#     echo ""
#     echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
#     echo -e "${BLUE}║${NC}${BOLD}            ⚔️  WRK BATTLE TEST - NestJS vs Go  ⚔️              ${NC}${BLUE}║${NC}"
#     echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
#     echo ""
# }
#
# print_section() {
#     echo ""
#     echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
#     echo -e "${CYAN}  $1${NC}"
#     echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
# }
#
# check_wrk() {
#     if ! command -v wrk &> /dev/null; then
#         echo -e "${RED}❌ wrk is not installed${NC}"
#         echo ""
#         echo "Install wrk:"
#         echo "  macOS:  brew install wrk"
#         echo "  Ubuntu: sudo apt-get install wrk"
#         exit 1
#     fi
# }
#
# check_server() {
#     local url=$1
#     local name=$2
#
#     # Try health endpoint first, then root
#     local status=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 2 "$url/health" 2>/dev/null || echo "000")
#
#     if [ "$status" = "200" ]; then
#         echo -e "${GREEN}✅ $name is running at $url${NC}"
#         return 0
#     else
#         # Try a simple GET
#         status=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 2 "$url" 2>/dev/null || echo "000")
#         if [ "$status" != "000" ]; then
#             echo -e "${GREEN}✅ $name is running at $url (status: $status)${NC}"
#             return 0
#         fi
#     fi
#
#     echo -e "${RED}❌ $name is NOT running at $url${NC}"
#     return 1
# }
#
# # Extract metrics from wrk output
# extract_rps() {
#     echo "$1" | grep "Requests/sec" | awk '{print $2}'
# }
#
# extract_latency() {
#     echo "$1" | grep -A1 "Thread Stats" | tail -1 | awk '{print $2}'
# }
#
# extract_latency_p99() {
#     echo "$1" | grep "99%" | awk '{print $2}' || echo "N/A"
# }
#
# # ==================== TEST FUNCTIONS ====================
# run_get_test() {
#     local url=$1
#     local name=$2
#
#     echo -e "\n${YELLOW}▶ Testing $name - GET /v1/users/:id/overview${NC}"
#     echo "  Threads: $THREADS | Connections: $CONNECTIONS | Duration: $DURATION"
#
#     # Get first user ID from data file
#     local user_id=$(head -1 data/user_ids.txt)
#     local endpoint="$url/v1/users/$user_id/overview?page=1&limit=20"
#
#     local result=$(wrk -t$THREADS -c$CONNECTIONS -d$DURATION "$endpoint" 2>&1)
#     echo "$result"
#
#     # Return RPS
#     extract_rps "$result"
# }
#
# run_post_test() {
#     local url=$1
#     local name=$2
#
#     echo -e "\n${YELLOW}▶ Testing $name - POST /v1/checkout${NC}"
#     echo "  Threads: $THREADS | Connections: $CONNECTIONS | Duration: $DURATION"
#
#     local result=$(wrk -t$THREADS -c$CONNECTIONS -d$DURATION -s "$SCRIPT_DIR/post_checkout.lua" "$url" 2>&1)
#     echo "$result"
#
#     extract_rps "$result"
# }
#
# run_mixed_test() {
#     local url=$1
#     local name=$2
#
#     echo -e "\n${YELLOW}▶ Testing $name - Mixed Workload (90% GET / 10% POST)${NC}"
#     echo "  Threads: $THREADS | Connections: $CONNECTIONS | Duration: $DURATION"
#
#     local result=$(wrk -t$THREADS -c$CONNECTIONS -d$DURATION -s "$SCRIPT_DIR/mixed_workload.lua" "$url" 2>&1)
#     echo "$result"
#
#     extract_rps "$result"
# }
#
# # ==================== COMPARISON DISPLAY ====================
# print_comparison() {
#     local test_name=$1
#     local nestjs_rps=$2
#     local go_rps=$3
#
#     # Calculate winner and percentage difference
#     local winner=""
#     local diff=""
#
#     if (( $(echo "$go_rps > $nestjs_rps" | bc -l) )); then
#         winner="Go/Fiber"
#         if (( $(echo "$nestjs_rps > 0" | bc -l) )); then
#             diff=$(echo "scale=1; (($go_rps - $nestjs_rps) / $nestjs_rps) * 100" | bc)
#         else
#             diff="∞"
#         fi
#         echo -e "  ${MAGENTA}$test_name:${NC}"
#         echo -e "    NestJS:    ${nestjs_rps} req/s"
#         echo -e "    Go/Fiber:  ${go_rps} req/s ${GREEN}← Winner (+${diff}%)${NC}"
#     else
#         winner="NestJS"
#         if (( $(echo "$go_rps > 0" | bc -l) )); then
#             diff=$(echo "scale=1; (($nestjs_rps - $go_rps) / $go_rps) * 100" | bc)
#         else
#             diff="∞"
#         fi
#         echo -e "  ${MAGENTA}$test_name:${NC}"
#         echo -e "    NestJS:    ${nestjs_rps} req/s ${GREEN}← Winner (+${diff}%)${NC}"
#         echo -e "    Go/Fiber:  ${go_rps} req/s"
#     fi
# }
#
# print_final_results() {
#     echo ""
#     echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
#     echo -e "${BLUE}║${NC}${BOLD}                    📊 BATTLE RESULTS 📊                        ${NC}${BLUE}║${NC}"
#     echo -e "${BLUE}╠════════════════════════════════════════════════════════════════╣${NC}"
#     echo -e "${BLUE}║${NC}  Framework     │ GET req/s  │ POST req/s │ Mixed req/s      ${BLUE}║${NC}"
#     echo -e "${BLUE}╠════════════════════════════════════════════════════════════════╣${NC}"
#     printf "${BLUE}║${NC}  %-13s │ %10s │ %10s │ %10s       ${BLUE}║${NC}\n" "NestJS" "$NESTJS_GET_RPS" "$NESTJS_POST_RPS" "$NESTJS_MIXED_RPS"
#     printf "${BLUE}║${NC}  %-13s │ %10s │ %10s │ %10s       ${BLUE}║${NC}\n" "Go/Fiber" "$GO_GET_RPS" "$GO_POST_RPS" "$GO_MIXED_RPS"
#     echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
#
#     echo ""
#     echo -e "${BOLD}Performance Comparison:${NC}"
#     echo ""
#
#     print_comparison "GET Endpoint" "$NESTJS_GET_RPS" "$GO_GET_RPS"
#     echo ""
#     print_comparison "POST Endpoint" "$NESTJS_POST_RPS" "$GO_POST_RPS"
#     echo ""
#     print_comparison "Mixed Workload" "$NESTJS_MIXED_RPS" "$GO_MIXED_RPS"
#
#     echo ""
#     echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
#     echo -e "${BOLD}Test Configuration:${NC}"
#     echo "  Duration: $DURATION | Threads: $THREADS | Connections: $CONNECTIONS"
#     echo "  NestJS: $NESTJS_URL (port 3000)"
#     echo "  Go:     $GO_URL (port 3001)"
#     echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
# }
#
# # ==================== MAIN ====================
# main() {
#     local test_type="${1:-full}"
#
#     print_banner
#     check_wrk
#
#     echo -e "${BOLD}Checking servers...${NC}"
#
#     NESTJS_OK=true
#     GO_OK=true
#
#     check_server "$NESTJS_URL" "$NESTJS_NAME" || NESTJS_OK=false
#     check_server "$GO_URL" "$GO_NAME" || GO_OK=false
#
#     if [ "$NESTJS_OK" = false ] && [ "$GO_OK" = false ]; then
#         echo ""
#         echo -e "${RED}❌ Neither server is running!${NC}"
#         echo ""
#         echo "Please start your servers:"
#         echo "  NestJS: should be running on port 3000"
#         echo "  Go:     should be running on port 3001"
#         exit 1
#     fi
#
#     if [ "$NESTJS_OK" = false ]; then
#         echo -e "${YELLOW}⚠️  Only Go/Fiber will be tested${NC}"
#     fi
#
#     if [ "$GO_OK" = false ]; then
#         echo -e "${YELLOW}⚠️  Only NestJS will be tested${NC}"
#     fi
#
#     # Quick test mode
#     if [ "$test_type" = "quick" ]; then
#         DURATION="10s"
#         THREADS=4
#         CONNECTIONS=50
#         echo -e "\n${YELLOW}Running quick test (10s, 50 connections)${NC}"
#     fi
#
#     echo ""
#     echo -e "${BOLD}Test Parameters:${NC} Duration=$DURATION, Threads=$THREADS, Connections=$CONNECTIONS"
#
#     # ==================== GET TESTS ====================
#     print_section "📥 GET /v1/users/:id/overview"
#
#     if [ "$NESTJS_OK" = true ]; then
#         NESTJS_GET_RPS=$(run_get_test "$NESTJS_URL" "$NESTJS_NAME")
#     fi
#
#     sleep 3  # Cool down between tests
#
#     if [ "$GO_OK" = true ]; then
#         GO_GET_RPS=$(run_get_test "$GO_URL" "$GO_NAME")
#     fi
#
#     # ==================== POST TESTS ====================
#     print_section "📤 POST /v1/checkout"
#
#     sleep 3
#
#     if [ "$NESTJS_OK" = true ]; then
#         NESTJS_POST_RPS=$(run_post_test "$NESTJS_URL" "$NESTJS_NAME")
#     fi
#
#     sleep 3
#
#     if [ "$GO_OK" = true ]; then
#         GO_POST_RPS=$(run_post_test "$GO_URL" "$GO_NAME")
#     fi
#
#     # ==================== MIXED TESTS ====================
#     print_section "🔄 Mixed Workload (90% GET / 10% POST)"
#
#     sleep 3
#
#     if [ "$NESTJS_OK" = true ]; then
#         NESTJS_MIXED_RPS=$(run_mixed_test "$NESTJS_URL" "$NESTJS_NAME")
#     fi
#
#     sleep 3
#
#     if [ "$GO_OK" = true ]; then
#         GO_MIXED_RPS=$(run_mixed_test "$GO_URL" "$GO_NAME")
#     fi
#
#     # ==================== RESULTS ====================
#     print_section "🏆 FINAL RESULTS"
#     print_final_results
# }
#
# # Run
# case "$1" in
#     help|--help|-h)
#         echo "WRK Battle Test - NestJS vs Go Performance Comparison"
#         echo ""
#         echo "Usage: ./battle.sh [test_type] [duration]"
#         echo ""
#         echo "Test Types:"
#         echo "  quick    - Quick 10s test with 50 connections"
#         echo "  full     - Full test (default 30s, 200 connections)"
#         echo ""
#         echo "Examples:"
#         echo "  ./battle.sh              # Full test, 30s"
#         echo "  ./battle.sh quick        # Quick test, 10s"
#         echo "  ./battle.sh full 60      # Full test, 60s"
#         echo ""
#         echo "Server Configuration:"
#         echo "  NestJS: http://localhost:3000"
#         echo "  Go:     http://localhost:3001"
#         ;;
#     *)
#         main "$@"
#         ;;
# esac
#
#
#


#!/bin/bash
# WRK Battle Test - NestJS vs Go Performance Comparison
set -e

# ==================== CONFIGURATION ====================
NESTJS_URL="http://localhost:3000"
GO_URL="http://localhost:3001"

DURATION="${2:-30}s"
THREADS=12
CONNECTIONS=200

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
NC='\033[0m'

# ==================== FIX: ROBUST EXTRACTION ====================
extract_rps() {
    # Filters out non-numeric chars to prevent 'bc' parse errors
    local val=$(echo "$1" | grep "Requests/sec" | awk '{print $2}' | sed 's/[^0-9.]//g')
    echo "${val:-0}"
}

# ==================== TEST ENGINE ====================
run_test() {
    local label=$1
    local url=$2
    local extra_args=$3

    printf "\n${YELLOW}▶ Testing $label...${NC}\n" >&2

    # Run wrk and capture output
    local out=$(wrk -t$THREADS -c$CONNECTIONS -d$DURATION $extra_args "$url" 2>&1 | tee /dev/tty)

    # Check for socket errors in the output
    if echo "$out" | grep -iq "Socket errors"; then
        printf "${RED}⚠️  Warning: $label had socket errors. Results are bottlenecked by OS/Network.${NC}\n" >&2
    fi

    extract_rps "$out"
}

# ==================== COMPARISON LOGIC ====================
print_comparison() {
    local name=$1
    local nest=$2
    local go=$3

    # Ensure we are comparing numbers, default to 0 if empty
    nest=${nest:-0}
    go=${go:-0}

    printf "  ${MAGENTA}$name:${NC}\n"
    if (( $(echo "$go > $nest" | bc -l) )); then
        local diff=$(echo "scale=1; (($go - $nest) / $nest) * 100" | bc 2>/dev/null || echo "0")
        printf "    NestJS:   %10s req/s\n" "$nest"
        printf "    Go/Fiber: %10s req/s ${GREEN}← Winner (+${diff}%%)${NC}\n" "$go"
    else
        local diff=$(echo "scale=1; (($nest - $go) / $go) * 100" | bc 2>/dev/null || echo "0")
        printf "    NestJS:   %10s req/s ${GREEN}← Winner (+${diff}%%)${NC}\n" "$nest"
        printf "    Go/Fiber: %10s req/s\n" "$go"
    fi
}

# ==================== MAIN ====================
main() {
    # 1. GET Test
    printf "${CYAN}📥 TEST: GET OVERVIEW${NC}\n"
    local get_path="/v1/users/aadcb915-ce42-4e0d-b7c8-a11ee687478f/overview?page=1&limit=20"
    NEST_GET=$(run_test "NestJS" "$NESTJS_URL$get_path")
    GO_GET=$(run_test "Go/Fiber" "$GO_URL$get_path")

    # 2. MIXED Test (Lua)
    printf "\n${CYAN}🔄 TEST: MIXED WORKLOAD${NC}\n"
    # Note: Ensure mixed_workload.lua exists and has NO syntax errors
    NEST_MIX=$(run_test "NestJS" "$NESTJS_URL" "-s mixed_workload.lua")
    GO_MIX=$(run_test "Go/Fiber" "$GO_URL" "-s mixed_workload.lua")

    # Final Summary
    printf "\n${BOLD}🏆 FINAL BATTLE RESULTS${NC}\n"
    print_comparison "GET Results  " "$NEST_GET" "$GO_GET"
    print_comparison "Mixed Results" "$NEST_MIX" "$GO_MIX"
}

main "$@"
