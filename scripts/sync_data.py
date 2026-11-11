#!/usr/bin/env python
"""
数据同步脚本 — 从 AkShare 拉取基金数据并写入 SQLite

用法：
    python scripts/sync_data.py              # 拉取基金列表并写入
    python scripts/sync_data.py --nav 000001 # 拉取单只基金净值历史
    python scripts/sync_data.py --watch      # 巡检自选基金

依赖：pip install -r requirements.txt
"""

import argparse
import sys
from pathlib import Path

# 确保项目根目录在 sys.path 中
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from loguru import logger

logger.add(
    Path(__file__).resolve().parents[1] / "logs" / "sync.log",
    rotation="1 day",
    retention="7 days",
)

from src.data.akshare_source import AkShareSource
from src.db.repository import Repository
from src.monitoring.watchdog import FundWatchdog


def sync_fund_list(repo: Repository, source: AkShareSource):
    """同步全市场基金列表和最新净值"""
    logger.info("=" * 50)
    logger.info("开始同步: 全市场基金列表")
    logger.info("=" * 50)

    df = source.fetch_fund_list()
    logger.info(f"获取到 {len(df)} 只基金")

    count = repo.upsert_funds(df)
    logger.success(f"写入完成: {count} 条记录")


def sync_nav_history(repo: Repository, source: AkShareSource, fund_code: str):
    """同步单只基金历史净值"""
    logger.info(f"开始同步: 基金 {fund_code} 净值历史")

    df = source.fetch_nav_history(fund_code)
    if df.empty:
        logger.warning(f"基金 {fund_code} 无数据")
        return

    count = repo.upsert_nav_records(fund_code, df)
    logger.success(f"写入完成: {count} 条净值记录")


def run_watchdog(repo: Repository, source: AkShareSource, interval: int = 30):
    """运行监控引擎（Ctrl+C 退出）"""
    logger.info(f"启动监控引擎 (间隔={interval}分钟) ...")
    watchdog = FundWatchdog(repo, source)
    watchdog.start(interval_minutes=interval)
    try:
        import time
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        watchdog.stop()
        logger.info("监控已退出")


def main():
    parser = argparse.ArgumentParser(description="Fund Killer — 数据同步脚本")
    parser.add_argument(
        "--nav", type=str, default=None,
        help="拉取指定基金净值历史（如 --nav 000001）",
    )
    parser.add_argument(
        "--watch", action="store_true",
        help="启动定时监控引擎",
    )
    parser.add_argument(
        "--interval", type=int, default=30,
        help="监控巡检间隔（分钟），默认30",
    )
    args = parser.parse_args()

    source = AkShareSource()
    repo = Repository()

    try:
        if args.watch:
            run_watchdog(repo, source, interval=args.interval)
        elif args.nav:
            sync_nav_history(repo, source, args.nav)
        else:
            sync_fund_list(repo, source)
    except Exception as e:
        logger.error(f"同步失败: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
