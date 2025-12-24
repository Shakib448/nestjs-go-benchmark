-- Mixed Workload Test (90% GET / 10% POST)
-- Usage: wrk -t12 -c200 -d30s -s mixed_workload.lua http://localhost:3000

local GET_RATIO = 0.9

-- Load IDs from files
local user_ids = {}
local product_ids = {}

local function load_file(filename)
    local ids = {}
    local file = io.open(filename, "r")
    if file then
        for line in file:lines() do
            -- FIX: gsub returns two values (string, count).
            -- We must isolate the string first to avoid confusing table.insert.
            local clean_line = line:gsub("%s+", "")
            if clean_line ~= "" and clean_line:match("^%x") then
                table.insert(ids, (clean_line))
            end
        end
        file:close()
    end
    return ids
end

-- Try relative and absolute paths for your M1 file system
user_ids = load_file("data/user_ids.txt")
if #user_ids == 0 then
    user_ids = load_file("wrk-tests/data/user_ids.txt")
end
-- Fallback IDs if files are missing
if #user_ids == 0 then
    user_ids = {
        "50c9a500-fbe6-4c78-9661-49c13d9cebfb",
        "aff6131a-344e-4fa4-948f-c3ccd25bfd79",
        "bf086c58-d2e4-4ee3-854f-138700a8842f",
        "89ead645-4ff9-49f3-90d5-dc5fc360b51c",
        "68d37dac-37c3-4ec9-b79b-f393e355d23e",
    }
end

product_ids = load_file("data/product_ids.txt")
if #product_ids == 0 then
    product_ids = load_file("wrk-tests/data/product_ids.txt")
end
if #product_ids == 0 then
    product_ids = {
        "795d31a1-4709-4199-9ecd-d31e73c199c4",
        "89382300-e57b-448f-affd-d9d3880a29b1",
        "0defe14d-a6fe-48e6-8e12-fe4ef4f0673a",
        "063b5f77-c7e5-4773-af09-e5eef93a4908",
        "d5086e2f-4b8c-415b-8140-020c7e8ce783",
    }
end

local category_ids = {
    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
    "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
    "cccccccc-cccc-cccc-cccc-cccccccccccc",
    "dddddddd-dddd-dddd-dddd-dddddddddddd",
}

-- UUID generator for idempotency keys
local function uuid()
    local template = "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx"
    return string.gsub(template, "[xy]", function(c)
        local v = (c == "x") and math.random(0, 15) or math.random(8, 11)
        return string.format("%x", v)
    end)
end

-- Generate GET request
local function make_get()
    local user_id = user_ids[math.random(#user_ids)]
    local page = math.random(1, 5)
    local limit = ({ 10, 20, 50 })[math.random(3)]

    local path = string.format(
        "/v1/users/%s/overview?page=%d&limit=%d",
        user_id,
        page,
        limit
    )

    if math.random() > 0.5 then
        path = path
            .. "&categoryId="
            .. category_ids[math.random(#category_ids)]
    end

    return wrk.format("GET", path)
end

-- Generate POST request
local function make_post()
    local user_id = user_ids[math.random(#user_ids)]
    local idempotency_key = uuid()

    local items = {}
    local used = {}
    -- Pick 1 to 3 unique items
    for i = 1, math.random(1, 3) do
        local pid
        repeat
            pid = product_ids[math.random(#product_ids)]
        until not used[pid]
        used[pid] = true

        table.insert(
            items,
            string.format(
                '{"product_id":"%s","qty":%d}',
                pid,
                math.random(1, 2)
            )
        )
    end

    local body = string.format(
        '{"user_id":"%s","cart_id":"%s","items":[%s],"idempotency_key":"%s"}',
        user_id,
        uuid(),
        table.concat(items, ","),
        idempotency_key
    )

    return wrk.format("POST", "/v1/checkout", {
        ["Content-Type"] = "application/json",
        ["X-Idempotency-Key"] = idempotency_key,
    }, body)
end

-- Standard wrk request function
function request()
    if math.random() < GET_RATIO then
        return make_get()
    else
        return make_post()
    end
end

-- Reporting logic
function done(summary, latency, requests)
    local total = summary.requests
    local errors = summary.errors.status
        + summary.errors.connect
        + summary.errors.read
        + summary.errors.write
        + summary.errors.timeout
    local success = total - errors
    local rps = total / (summary.duration / 1000000)

    io.stderr:write("\n")
    io.stderr:write(
        "================================================================================\n"
    )
    io.stderr:write("                    MIXED WORKLOAD TEST RESULTS\n")
    io.stderr:write(
        "================================================================================\n"
    )
    io.stderr:write(string.format("  Total Requests:      %d\n", total))
    io.stderr:write(
        string.format(
            "  Successful:          %d (%.1f%%)\n",
            success,
            (success / total) * 100
        )
    )
    io.stderr:write(string.format("  Errors:              %d\n", errors))
    io.stderr:write(
        string.format("    - Status (Non-2xx): %d\n", summary.errors.status)
    )
    io.stderr:write(
        string.format("    - Timeout:         %d\n", summary.errors.timeout)
    )
    io.stderr:write("\n")
    io.stderr:write("  Latency (ms):\n")
    io.stderr:write(
        string.format("    Mean:              %.2f\n", latency.mean / 1000)
    )
    io.stderr:write(
        string.format(
            "    p50:               %.2f\n",
            latency:percentile(50) / 1000
        )
    )
    io.stderr:write(
        string.format(
            "    p90:               %.2f\n",
            latency:percentile(90) / 1000
        )
    )
    io.stderr:write(
        string.format(
            "    p99:               %.2f\n",
            latency:percentile(99) / 1000
        )
    )
    io.stderr:write("\n")
    io.stderr:write(string.format("  Throughput (RPS):    %.2f\n", rps))
    io.stderr:write(
        "================================================================================\n"
    )
end
