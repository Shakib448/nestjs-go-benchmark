-- POST Checkout Test
-- Usage: wrk -t8 -c100 -d30s -s post_checkout.lua http://localhost:3000

-- 1. Load Data from Files
local function load_file(filename)
    local ids = {}
    local file = io.open(filename, "r")
    if file then
        for line in file:lines() do
            -- Clean the line: remove whitespace
            local clean_line = line:gsub("%s+", "")
            -- Only insert if it looks like a valid ID (hex/uuid start)
            if clean_line ~= "" and clean_line:match("^%x") then
                table.insert(ids, (clean_line))
            end
        end
        file:close()
    end
    return ids
end

-- Try loading from multiple possible paths (adjust for your folder structure)
local user_ids = load_file("data/user_ids.txt")
if #user_ids == 0 then
    user_ids = load_file("wrk-tests/data/user_ids.txt")
end

local product_ids = load_file("data/product_ids.txt")
if #product_ids == 0 then
    product_ids = load_file("wrk-tests/data/product_ids.txt")
end

-- SAFETY FALLBACK: Use these if files are not found
if #user_ids == 0 then
    user_ids = { "50c9a500-fbe6-4c78-9661-49c13d9cebfb" }
end
if #product_ids == 0 then
    product_ids = { "795d31a1-4709-4199-9ecd-d31e73c199c4" }
end

-- 2. UUID Generator
local function uuid()
    local template = "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx"
    return string.gsub(template, "[xy]", function(c)
        local v = (c == "x") and math.random(0, 15) or math.random(8, 11)
        return string.format("%x", v)
    end)
end

-- 3. Request logic
function request()
    local user_id = user_ids[math.random(#user_ids)]
    local idempotency_key = uuid()
    local cart_id = uuid()

    local items = {}
    for i = 1, math.random(1, 3) do
        local pid = product_ids[math.random(#product_ids)]
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
        cart_id,
        table.concat(items, ","),
        idempotency_key
    )

    return wrk.format("POST", "/v1/checkout", {
        ["Content-Type"] = "application/json",
        ["X-Idempotency-Key"] = idempotency_key,
    }, body)
end

-- 4. Results Reporting
function done(summary, latency, requests)
    local total_req = summary.requests
    local error_count = summary.errors.status
        + summary.errors.connect
        + summary.errors.read
        + summary.errors.write
        + summary.errors.timeout

    local success_count = total_req - error_count
    local rps = total_req / (summary.duration / 1000000)

    io.stderr:write("\n" .. string.rep("=", 60) .. "\n")
    io.stderr:write("                 BENCHMARK RESULTS\n")
    io.stderr:write(string.rep("=", 60) .. "\n")
    io.stderr:write(string.format("Total Requests:   %d\n", total_req))
    io.stderr:write(
        string.format(
            "Successful:       %d (%.1f%%)\n",
            success_count,
            (success_count / total_req) * 100
        )
    )
    io.stderr:write(string.format("Errors:           %d\n", error_count))
    io.stderr:write(string.format("Throughput (RPS): %.2f\n", rps))
    io.stderr:write(string.format("Avg Latency:      %.2f ms\n", latency.mean / 1000))
    io.stderr:write(
        string.format(
            "P99 Latency:      %.2f ms\n",
            latency:percentile(99) / 1000
        )
    )
    io.stderr:write(string.rep("=", 60) .. "\n")
end
