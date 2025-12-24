#!/bin/bash
#
# WRK Performance Battle: NestJS vs. Go/Fiber
#
# This script runs a series of load tests against two applications
# to compare their performance under different workloads. It uses
# the wrk tool and custom Lua scripts to simulate realistic traffic.
#

set -e
trap 'echo "An error occurred. Exiting..."; exit 1;' ERR

# ==================== CONFIGURATION ====================
# --- Framework URLs ---
NESTJS_URL="http://localhost:3000"
GO_URL="http://localhost:3001"
NESTJS_NAME="NestJS"
GO_NAME="Go/Fiber"

# --- Test Parameters ---
# Duration of each test (e.g., 30s, 1m, 10s)
DURATION="${1:-30s}"
# Number of threads to use
THREADS=12
# Number of concurrent connections to keep open
CONNECTIONS=200

# --- Script Directory ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- Colors for Logging ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# ==================== HELPER FUNCTIONS ====================

# Prints a section header.
print_section() {
    echo -e "\n${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${CYAN}  $1${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

# Checks if wrk is installed on the system.
check_wrk() {
    if ! command -v wrk &> /dev/null; then
        echo -e "${RED}❌ Error: wrk is not installed.${NC}"
        echo "Please install it to continue (e.g., 'brew install wrk' on macOS)."
        exit 1
    fi
}

# Checks if a server is responsive.
check_server() {
    local url=$1
    local name=$2
    if curl -v -o /dev/null --fail --connect-timeout 2 "$url/health"; then
        echo -e "${GREEN}✅ $name server is responsive at $url${NC}"
        return 0
    else
        echo -e "${RED}❌ $name server is NOT running or reachable at $url${NC}"
        return 1
    fi
}

# ==================== TEST ENGINE ====================

# A robust function to run a single wrk test and extract results.
# Arguments: $1: Framework Name, $2: URL, $3: Lua Script
run_single_test() {
    local name="$1"
    local url="$2"
    local lua_script="$3"

    echo -e "\n${YELLOW}▶ Testing ${BOLD}$name${NC} with ${BOLD}$lua_script${NC}..." >&2
    echo "  Duration: $DURATION, Threads: $THREADS, Connections: $CONNECTIONS" >&2
    
    # Execute wrk and capture its output. Redirect stderr to stdout.
    local result
    result=$(wrk -t"$THREADS" -c"$CONNECTIONS" -d"$DURATION" -s "$SCRIPT_DIR/$lua_script" "$url" 2>&1)
    
    # Display the full output from wrk for transparency.
    echo "$result" >&2 # Direct full wrk output to stderr

    # Check for socket errors, which indicate a bottleneck.
    if echo "$result" | grep -q "Socket errors"; then
        echo -e "${RED}⚠️  Warning: Socket errors detected. The server may be overloaded or misconfigured.${NC}" >&2
    fi

    # Extract Requests/sec, defaulting to "0.00" on failure.
    local rps
    rps=$(echo "$result" | grep -E "Requests/sec:|Throughput \(RPS\):" | awk '{print $NF}' | tail -n 1)
    echo "${rps:-0.00}" # Only the RPS value goes to stdout
}

# ==================== RESULTS DISPLAY ====================

# Prints a formatted comparison of two results.
# Arguments: $1: Test Name, $2: NestJS RPS, $3: Go RPS
print_comparison() {
    local test_name="$1"
    local nest_rps="$2"
    local go_rps="$3" # Fixed: Added missing double quote
    
    # Trim potential whitespace/newlines from RPS values to ensure clean input for bc
    nest_rps=$(echo "$nest_rps" | tr -d '[:space:]')
    go_rps=$(echo "$go_rps" | tr -d '[:space:]')

    # DEBUG: Print raw and trimmed values
    echo "DEBUG: Raw nest_rps = ['$nest_rps']" >&2
    echo "DEBUG: Raw go_rps = ['$go_rps']" >&2
    
    # Trim potential whitespace/newlines from RPS values to ensure clean input for bc
    nest_rps_trimmed=$(echo "$nest_rps" | tr -d '[:space:]')
    go_rps_trimmed=$(echo "$go_rps" | tr -d '[:space:]')

    echo "DEBUG: Trimmed nest_rps = ['$nest_rps_trimmed']" >&2
    echo "DEBUG: Trimmed go_rps = ['$go_rps_trimmed']" >&2

    # Use bc for floating point comparison.
    # Check if numbers are valid before comparison to avoid bc errors
    if [[ "$go_rps_trimmed" =~ ^[0-9.]+$ ]] && [[ "$nest_rps_trimmed" =~ ^[0-9.]+$ ]]; then
        if (( $(echo "$go_rps_trimmed > $nest_rps_trimmed" | bc -l) )); then
            local diff
            diff=$(echo "scale=1; (($go_rps_trimmed - $nest_rps_trimmed) / $nest_rps_trimmed) * 100" | bc 2>/dev/null || echo "0")
            echo -e "  Go/Fiber:  ${go_rps_trimmed} req/s ${GREEN}(Winner, +${diff}%)${NC}"
            echo -e "  NestJS:    ${nest_rps_trimmed} req/s"
        else
            local diff
            diff=$(echo "scale=1; (($nest_rps_trimmed - $go_rps_trimmed) / $go_rps_trimmed) * 100" | bc 2>/dev/null || echo "0")
            echo -e "  NestJS:    ${nest_rps_trimmed} req/s ${GREEN}(Winner, +${diff}%)${NC}"
            echo -e "  Go/Fiber:  ${go_rps_trimmed} req/s"
        fi
    else
        echo -e "  Go/Fiber:  ${go_rps_trimmed} req/s (Invalid RPS value)"
        echo -e "  NestJS:    ${nest_rps_trimmed} req/s (Invalid RPS value)"
    fi
}

# ==================== MAIN EXECUTION ====================

main() {
    print_section "SETUP & SERVER CHECK"
    check_wrk
    check_server "$NESTJS_URL" "$NESTJS_NAME"
    check_server "$GO_URL" "$GO_NAME"
    
    local nest_get_rps nest_post_rps nest_mixed_rps
    local go_get_rps go_post_rps go_mixed_rps

    # --- Test 1: GET Overview ---
    print_section "TEST 1: GET WORKLOAD (get_overview.lua)"
    nest_get_rps=$(run_single_test "$NESTJS_NAME" "$NESTJS_URL" "get_overview.lua")
    sleep 5 # Cooldown period
    go_get_rps=$(run_single_test "$GO_NAME" "$GO_URL" "get_overview.lua")

    # --- Test 2: POST Checkout ---
    print_section "TEST 2: POST WORKLOAD (post_checkout.lua)"
    nest_post_rps=$(run_single_test "$NESTJS_NAME" "$NESTJS_URL" "post_checkout.lua")
    sleep 5 # Cooldown period
    go_post_rps=$(run_single_test "$GO_NAME" "$GO_URL" "post_checkout.lua")

    # --- Test 3: Mixed Workload ---
    print_section "TEST 3: MIXED WORKLOAD (mixed_workload.lua)"
    nest_mixed_rps=$(run_single_test "$NESTJS_NAME" "$NESTJS_URL" "mixed_workload.lua")
    sleep 5 # Cooldown period
    go_mixed_rps=$(run_single_test "$GO_NAME" "$GO_URL" "mixed_workload.lua")

    # --- Final Results ---
    print_section "🏆 FINAL BATTLE RESULTS 🏆"
    echo -e "${BOLD}GET Workload Comparison:${NC}"
    print_comparison "GET" "$nest_get_rps" "$go_get_rps"
    
    echo -e "\n${BOLD}POST Workload Comparison:${NC}"
    print_comparison "POST" "$nest_post_rps" "$go_post_rps"

    echo -e "\n${BOLD}Mixed Workload Comparison:${NC}"
    print_comparison "Mixed" "$nest_mixed_rps" "$go_mixed_rps"
    
    print_section "TEST COMPLETE"
}

# --- Run the main function ---
main "$@"
