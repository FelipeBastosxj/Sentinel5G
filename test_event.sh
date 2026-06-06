#!/bin/bash
echo '{"eventType":"STATUS_EVENT","channel":"sms","source":"custom","payload":{"msg":"clickhouse-test"},"metadata":{}}' | \
  curl -s -X POST http://localhost:3001/ingest \
    -H 'Content-Type: application/json' \
    -d @-
echo ""
sleep 3
echo "=== Processing logs ==="
docker logs --tail 8 eventstream-processing 2>&1
echo ""
echo "=== ClickHouse count ==="
curl -s "http://localhost:8123/?query=SELECT+count()+FROM+eventstream.events" \
  -H "X-ClickHouse-User: default" \
  -H "X-ClickHouse-Database: eventstream"
echo ""
echo "=== Recent events ==="
curl -s "http://localhost:8123/?query=SELECT+event_id,channel,source,timestamp+FROM+eventstream.events+LIMIT+3+FORMAT+JSON" \
  -H "X-ClickHouse-User: default" \
  -H "X-ClickHouse-Database: eventstream"
