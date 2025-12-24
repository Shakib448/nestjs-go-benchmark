-- GET Overview Test
-- Usage: wrk -t12 -c200 -d30s -s get_overview.lua http://localhost:3000

-- Load IDs from files
local user_ids = {}

local function load_file(filename)
    local ids = {}
    local file = io.open(filename, "r")
    if file then
        for line in file:lines() do
            if line ~= "" and line:match("^%x") then
                local clean_line = line:gsub("%s+", "")
                -- DEBUG: Print the value before inserting
                -- io.stderr:write("DEBUG: Inserting [" .. clean_line .. "] with type " .. type(clean_line) .. "\n")
                table.insert(ids, (line:gsub("%s+", "")))
            end
        end
        file:close()
    end
    return ids
end

user_ids = load_file("data/user_ids.txt")
if #user_ids == 0 then
    user_ids = load_file("wrk-tests/data/user_ids.txt")
end
if #user_ids == 0 then
    user_ids = {
        "50c9a500-fbe6-4c78-9661-49c13d9cebfb",
        "aff6131a-344e-4fa4-948f-c3ccd25bfd79",
        "bf086c58-d2e4-4ee3-854f-138700a8842f",
        "89ead645-4ff9-49f3-90d5-dc5fc360b51c",
        "68d37dac-37c3-4ec9-b79b-f393e355d23e",
    }
end

local category_ids = {
    "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
    "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
    "cccccccc-cccc-cccc-cccc-cccccccccccc",
    "dddddddd-dddd-dddd-dddd-dddddddddddd",
}

function request()
    local user_id = user_ids[math.random(#user_ids)]
    local page = math.random(1, 10)
    local limit = ({ 10, 20, 50 })[math.random(3)]

    local path = string.format(
        "/v1/users/%s/overview?page=%d&limit=%d",
        user_id,
        page,
        limit
    )

    -- 50% chance of category filter
    if math.random() > 0.5 then
        path = path
            .. "&categoryId="
            .. category_ids[math.random(#category_ids)]
    end

    return wrk.format("GET", path, { ["Content-Type"] = "application/json" })
end

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
    io.stderr:write("                       GET OVERVIEW TEST RESULTS\n")
    io.stderr:write(
        "================================================================================\n"
    )
    io.stderr:write(string.format("  Total Requests:     %d\n", total))
    io.stderr:write(
        string.format(
            "  Successful:         %d (%.1f%%)\n",
            success,
            (success / total) * 100
        )
    )
    io.stderr:write(string.format("  Errors:             %d\n", errors))
    io.stderr:write("\n")
    io.stderr:write("  Latency:\n")
    io.stderr:write(
        string.format("    Mean:             %.2f ms\n", latency.mean / 1000)
    )
    io.stderr:write(
        string.format("    Max:              %.2f ms\n", latency.max / 1000)
    )
    io.stderr:write(
        string.format(
            "    p50:              %.2f ms\n",
            latency:percentile(50) / 1000
        )
    )
    io.stderr:write(
        string.format(
            "    p90:              %.2f ms\n",
            latency:percentile(90) / 1000
        )
    )
    io.stderr:write(
        string.format(
            "    p99:              %.2f ms\n",
            latency:percentile(99) / 1000
        )
    )
    io.stderr:write("\n")
    io.stderr:write("  Throughput:\n")
    io.stderr:write(string.format("    Throughput (RPS): %.2f\n", rps))
    io.stderr:write(
        string.format(
            "    Transfer/sec:     %.2f MB\n",
            (summary.bytes / (summary.duration / 1000000)) / 1048576
        )
    )
    io.stderr:write(
        "================================================================================\n"
    )
end
