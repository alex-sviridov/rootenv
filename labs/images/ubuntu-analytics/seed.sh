#!/bin/bash
# Seeds the DataStream demo scenario at image build time (runs as root).
# Baked into the image rather than a runtime setup script because the
# container's default/only user (`lab`) has no root access at provision time.
set -e

# --- Структура каталогов ---
mkdir -p /app/datastream/data
mkdir -p /var/log/datastream
mkdir -p /var/lib/datastream/cache
mkdir -p /home/lab/reports

# --- Код и конфиг приложения ---
cat > /app/datastream/config.yaml << 'EOF'
server:
  host: 0.0.0.0
  port: 8080
  workers: 4

storage:
  data_dir: /app/datastream/data
  db_path: /var/lib/datastream/events.db
  cache_dir: /var/lib/datastream/cache

logging:
  access_log: /var/log/datastream/access.log
  error_log: /var/log/datastream/error.log
  level: info

retention:
  log_days: 30
  cache_ttl: 3600
EOF

cat > /app/datastream/requirements.txt << 'EOF'
flask==2.3.2
pandas==2.0.3
sqlalchemy==2.0.19
gunicorn==21.2.0
pyyaml==6.0.1
EOF

cat > /app/datastream/app.py << 'EOF'
# DataStream — HTTP event ingestion service
# Accepts CSV uploads, aggregates by client_id, writes reports to /var/lib/datastream/

import yaml
from flask import Flask, request, jsonify

app = Flask(__name__)

@app.route('/api/upload', methods=['POST'])
def upload():
    pass

@app.route('/api/status', methods=['GET'])
def status():
    return jsonify({"status": "ok"})

@app.route('/api/report/<client_id>', methods=['GET'])
def report(client_id):
    pass

if __name__ == '__main__':
    app.run()
EOF

# --- CSV-данные ---
cat > /app/datastream/data/events_2024_01.csv << 'EOF'
timestamp,client_id,event_type,value,region
2024-01-15 08:23:11,client_042,purchase,149.99,EU
2024-01-15 08:24:03,client_017,pageview,0,US
2024-01-15 08:25:47,client_042,purchase,89.50,EU
2024-01-15 08:26:12,client_103,signup,0,APAC
2024-01-15 08:27:01,client_017,purchase,299.00,US
2024-01-15 09:01:44,client_055,pageview,0,EU
2024-01-15 09:15:22,client_042,refund,149.99,EU
2024-01-15 10:03:09,client_017,purchase,59.90,US
2024-01-16 08:00:01,client_103,purchase,199.00,APAC
2024-01-16 08:45:33,client_055,purchase,79.00,EU
EOF

cat > /app/datastream/data/events_2024_02.csv << 'EOF'
timestamp,client_id,event_type,value,region
2024-02-01 07:12:00,client_017,purchase,349.00,US
2024-02-01 07:45:11,client_099,signup,0,EU
2024-02-02 09:30:22,client_042,purchase,249.99,EU
2024-02-03 10:11:05,client_099,purchase,89.00,EU
2024-02-05 14:00:00,client_103,refund,199.00,APAC
2024-02-07 16:22:44,client_055,purchase,120.00,EU
2024-02-10 08:05:33,client_017,purchase,59.90,US
EOF

cat > /app/datastream/data/clients.csv << 'EOF'
client_id,name,plan,region,since
client_017,Acme Corp,enterprise,US,2022-03-01
client_042,BrightData GmbH,pro,EU,2023-01-15
client_055,Solaris SAS,starter,EU,2023-07-20
client_099,NordMetrics,pro,EU,2024-01-05
client_103,AsiaPulse Ltd,enterprise,APAC,2022-11-30
EOF

# --- events.db: реальная SQLite база, а не бинарный stub ---
sqlite3 /var/lib/datastream/events.db << 'EOF'
CREATE TABLE clients (
    client_id TEXT PRIMARY KEY,
    name TEXT,
    plan TEXT,
    region TEXT,
    since TEXT
);
CREATE TABLE events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT,
    client_id TEXT,
    event_type TEXT,
    value REAL,
    region TEXT
);
.mode csv
.import /app/datastream/data/clients.csv clients_import
INSERT INTO clients SELECT * FROM clients_import WHERE client_id != 'client_id';
DROP TABLE clients_import;
.import /app/datastream/data/events_2024_01.csv events_import
.import /app/datastream/data/events_2024_02.csv events_import
INSERT INTO events (timestamp, client_id, event_type, value, region)
    SELECT timestamp, client_id, event_type, value, region FROM events_import
    WHERE timestamp != 'timestamp';
DROP TABLE events_import;
EOF

# --- Небольшой скрипт статуса сервиса, имитирующий живой хост ---
cat > /app/datastream/status.sh << 'EOF'
#!/bin/bash
# Прибитый к сервису health-check, вызывается извне мониторингом (демо-заглушка)
echo "datastream: up, workers=4, db=$(du -h /var/lib/datastream/events.db | cut -f1)"
EOF
chmod +x /app/datastream/status.sh

# --- Генерация access.log ---
python3 << 'PYEOF'
import random
import datetime

ips = [
    "185.220.101.42", "93.184.216.34", "198.51.100.17",
    "203.0.113.5",    "185.220.101.98", "91.108.4.11",
    "172.16.254.1",   "45.33.32.156",   "162.243.10.151",
    "104.21.14.177",  "185.220.101.42", "93.184.216.34",
]
endpoints_methods = [
    ("POST", "/api/upload"),   ("POST", "/api/upload"),
    ("POST", "/api/upload"),   ("GET",  "/api/status"),
    ("GET",  "/api/status"),   ("GET",  "/api/report/client_042"),
    ("GET",  "/api/report/client_017"), ("GET", "/api/report/client_103"),
    ("GET",  "/api/metrics"),  ("GET",  "/favicon.ico"),
    ("GET",  "/robots.txt"),
]
codes_by_endpoint = {
    "/api/upload":  ([200]*7 + [201]*2 + [400]*2 + [500]*2 + [503]*1),
    "/api/status":  ([200]*9 + [500]*1),
    "/api/report/client_042": ([200]*8 + [404]*1 + [500]*1),
    "/api/report/client_017": ([200]*8 + [404]*1 + [500]*1),
    "/api/report/client_103": ([200]*7 + [404]*2 + [500]*1),
    "/api/metrics": ([200]*8 + [403]*2),
    "/favicon.ico": ([200]*5 + [404]*5),
    "/robots.txt":  ([200]*9 + [404]*1),
}
user_agents = [
    "python-requests/2.28.0", "curl/7.88.1",
    "Mozilla/5.0 (compatible; DataStream-Client/1.2)",
    "Go-http-client/1.1",
]

random.seed(42)
start = datetime.datetime(2024, 3, 1, 0, 0, 0)
lines = []
t = start
for i in range(5000):
    t += datetime.timedelta(seconds=random.randint(5, 120))
    ip = random.choice(ips)
    method, endpoint = random.choice(endpoints_methods)
    pool = codes_by_endpoint.get(endpoint, [200]*8 + [500]*2)
    code = random.choice(pool)
    size = random.randint(512, 15000) if code < 400 else random.randint(50, 512)
    ua = random.choice(user_agents)
    ts = t.strftime("%d/%b/%Y:%H:%M:%S +0000")
    lines.append(
        f'{ip} - - [{ts}] "{method} {endpoint} HTTP/1.1" {code} {size} "-" "{ua}"'
    )

with open("/var/log/datastream/access.log", "w") as f:
    f.write("\n".join(lines) + "\n")
PYEOF

# --- Генерация error.log ---
python3 << 'PYEOF'
import random
import datetime

random.seed(7)
errors = [
    "Database connection timeout after 30s",
    "Failed to parse CSV: unexpected column count in row 47",
    "Client client_042 report generation failed: missing data for 2024-02",
    "Cache write failed: disk quota exceeded",
    "Request body too large: 12.4MB received, max 10MB allowed",
    "Upstream timeout for /api/upload after 60s",
    "Unhandled exception in report worker PID 1842",
]
warnings = [
    "Slow query detected: 2.3s for report generation client_103",
    "Cache hit ratio below threshold: 0.42 (expected >0.7)",
    "Memory usage above 80%: 817MB / 1024MB",
    "Retry 2/3 for client_103 webhook delivery",
    "Log file approaching rotation threshold: 480MB",
    "Worker respawn detected: PID 1839 exited with code 1",
]

start = datetime.datetime(2024, 3, 1, 0, 5, 0)
t = start
lines = []
for _ in range(450):
    t += datetime.timedelta(seconds=random.randint(30, 900))
    level = random.choices(["ERROR", "WARN"], weights=[35, 65])[0]
    msg = random.choice(errors if level == "ERROR" else warnings)
    ts = t.strftime("%Y-%m-%d %H:%M:%S")
    lines.append(f"[{ts}] [{level}] {msg}")

with open("/var/log/datastream/error.log", "w") as f:
    f.write("\n".join(lines) + "\n")
PYEOF

# --- Архивные логи (имитация logrotate) ---
cp /var/log/datastream/access.log /var/log/datastream/access.log.1
gzip -c /var/log/datastream/access.log.1 > /var/log/datastream/access.log.2.gz
rm /var/log/datastream/access.log.1

# --- Мусор: кэш-файлы (главный виновник) ---
dd if=/dev/zero of=/var/lib/datastream/cache/report_cache_2024_01.tmp bs=1M count=18 2>/dev/null
dd if=/dev/zero of=/var/lib/datastream/cache/report_cache_2024_02.tmp bs=1M count=22 2>/dev/null
dd if=/dev/zero of=/var/lib/datastream/cache/report_cache_2024_03.tmp bs=1M count=15 2>/dev/null

# --- Мусор: бэкапы и старый экспорт ---
cp /var/lib/datastream/events.db /var/lib/datastream/events.db.bak
dd if=/dev/zero of=/var/lib/datastream/old_export_2024_01.tar.gz bs=1M count=28 2>/dev/null
touch -d "45 days ago" /var/lib/datastream/old_export_2024_01.tar.gz
touch -d "45 days ago" /var/lib/datastream/events.db.bak

# --- Мусор в домашней папке сервиса ---
dd if=/dev/zero of=/home/datastream/deploy_bundle_v1.tar.gz bs=1M count=20 2>/dev/null
touch -d "65 days ago" /home/datastream/deploy_bundle_v1.tar.gz
cat > /home/datastream/migrate_db_v1.sh << 'EOF'
#!/bin/bash
# Old migration script from v1 -> v2 schema, no longer needed
sqlite3 /var/lib/datastream/events.db "ALTER TABLE events ADD COLUMN region TEXT"
EOF
touch -d "90 days ago" /home/datastream/migrate_db_v1.sh
dd if=/dev/zero of=/home/datastream/core.1839 bs=1M count=8 2>/dev/null
touch -d "3 days ago" /home/datastream/core.1839

# --- Права доступа ---
chown -R datastream:datastream /app/datastream /var/log/datastream /var/lib/datastream /home/datastream
chmod 644 /app/datastream/config.yaml /app/datastream/requirements.txt
chmod 640 /var/log/datastream/access.log /var/log/datastream/error.log
chmod -R 775 /home/datastream
chown -R lab:lab /home/lab
chmod 755 /home/lab/reports
usermod -a -G datastream lab
chmod -R g+w /app/datastream /var/log/datastream /var/lib/datastream /home/datastream

# Разрешаем lab читать логи
chmod o+r /var/log/datastream/access.log /var/log/datastream/error.log
chmod o+x /var/log/datastream
