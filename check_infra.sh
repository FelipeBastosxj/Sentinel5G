#!/bin/bash
set -e

echo "=== Prometheus Targets ==="
curl -s http://localhost:9090/api/v1/targets > /tmp/targets.json
python3 -c "
import json
d = json.load(open('/tmp/targets.json'))
for t in d['data']['activeTargets']:
    print(t['labels']['job'], '|', t['health'], '|', t.get('lastError', ''))
"

echo ""
echo "=== ClickHouse: count events ==="
curl -s "http://localhost:8123/?query=SELECT+count()+FROM+eventstream.events" -H "X-ClickHouse-User: default" -H "X-ClickHouse-Database: eventstream"

echo ""
echo "=== ClickHouse: recent events ==="
curl -s "http://localhost:8123/?query=SELECT+event_id,channel,source,timestamp+FROM+eventstream.events+ORDER+BY+timestamp+DESC+LIMIT+5+FORMAT+JSON" -H "X-ClickHouse-User: default" -H "X-ClickHouse-Database: eventstream"

echo ""
echo "=== Processing-service logs (last 30 lines) ==="
docker logs --tail 30 eventstream-processing 2>&1

echo ""
echo "=== Processing-service: /metrics endpoint ==="
curl -s http://localhost:3003/metrics | head -20

echo ""
echo "=== Ingestion-service: /metrics endpoint ==="
curl -s http://localhost:3001/metrics | head -10
