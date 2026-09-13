#!/usr/bin/env bash
set -euo pipefail
cd /Users/kstephenkeehn/aicon-mini/ai-cookbook/knowledge/hybrid-retrieval
# 1️⃣ setup a venv
python -m venv /tmp/venv
source /tmp/venv/bin/activate
pip install -q -r requirements.txt
# 2️⃣ use the same query set
cp ../../bench.yaml queries.yaml
# 3️⃣ run evaluation (it outputs a tab-separated result)
python evaluate.py -q queries.yaml -c 120 -k 5 > /tmp/baseline.tsv
# 4️⃣ copy the final table for CI
cp /tmp/baseline.tsv ../../baseline.tsv