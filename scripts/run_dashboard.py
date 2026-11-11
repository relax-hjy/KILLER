#!/usr/bin/env python
"""
启动 Streamlit 面板

用法：
    python scripts/run_dashboard.py

等效于：
    streamlit run src/ui/dashboard.py
"""

import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
DASHBOARD = PROJECT_ROOT / "src" / "ui" / "dashboard.py"

if not DASHBOARD.exists():
    print(f"错误: 找不到 {DASHBOARD}")
    sys.exit(1)

print(f"启动 Streamlit 面板...")
print(f"  文件: {DASHBOARD}")
print(f"  浏览器打开: http://localhost:8501")
print()

subprocess.run(
    [sys.executable, "-m", "streamlit", "run", str(DASHBOARD)],
    cwd=str(PROJECT_ROOT),
)
