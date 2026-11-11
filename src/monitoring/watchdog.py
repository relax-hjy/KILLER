"""
基金异动监控引擎

使用 APScheduler 定时巡检自选基金的最新净值，
当日涨跌幅超过阈值时触发告警并写入日志。
"""

from datetime import datetime
from typing import Optional

import pandas as pd
from apscheduler.schedulers.background import BackgroundScheduler
from loguru import logger

from src.data.akshare_source import AkShareSource
from src.data.base import DataSource
from src.db.repository import Repository


class Alert:
    """告警记录"""

    def __init__(
        self,
        fund_code: str,
        fund_name: str,
        alert_type: str,
        current_value: float,
        threshold: float,
        nav_date: str,
    ):
        self.fund_code = fund_code
        self.fund_name = fund_name
        self.alert_type = alert_type      # daily_change | weekly_change | drawdown
        self.current_value = current_value  # 当前值（%）
        self.threshold = threshold          # 阈值（%）
        self.nav_date = nav_date
        self.triggered_at = datetime.now()

    def __repr__(self):
        return (
            f"[⚠️ {self.alert_type}] {self.fund_name}({self.fund_code}) "
            f"当前={self.current_value:.2f}% 阈值={self.threshold:.2f}% "
            f"净值日={self.nav_date}"
        )


class FundWatchdog:
    """基金监控看门狗"""

    def __init__(
        self,
        repo: Repository,
        datasource: Optional[DataSource] = None,
    ):
        self.repo = repo
        self.datasource = datasource or AkShareSource()
        self.scheduler = BackgroundScheduler()
        self.alerts: list[Alert] = []

    def check_watch_items(self) -> list[Alert]:
        """
        巡检所有自选基金，对比最新净值与数据库中最近一条记录

        Returns
        -------
        list[Alert] : 触发的告警列表
        """
        watch_list = self.repo.get_watch_list()
        if not watch_list:
            logger.info("[Watchdog] 自选列表为空，跳过巡检")
            return []

        new_alerts: list[Alert] = []
        logger.info(f"[Watchdog] 巡检 {len(watch_list)} 只自选基金 ...")

        for item in watch_list:
            try:
                alert = self._check_single(item.fund_code, item.fund_name, item.alert_daily_pct)
                if alert:
                    new_alerts.append(alert)
                    logger.warning(str(alert))
            except Exception as e:
                logger.error(f"[Watchdog] 巡检 {item.fund_code} 异常: {e}")

        self.alerts.extend(new_alerts)
        logger.info(f"[Watchdog] 巡检完成: {len(new_alerts)} 条告警")
        return new_alerts

    def _check_single(
        self, fund_code: str, fund_name: str, threshold_pct: float
    ) -> Optional[Alert]:
        """
        检查单只基金

        策略：
        1. 从数据源拉取最新日度数据
        2. 对比数据库中最近一条 NavRecord
        3. 如果日涨跌幅超过阈值 → 告警
        """
        try:
            df = self.datasource.fetch_nav_history(fund_code)
            if df.empty:
                return None

            # 最新净值行
            latest = df.iloc[-1]
            nav_date = str(latest.get("nav_date", ""))
            daily_return = self._safe_float(latest.get("daily_return", 0))

            # 对比阈值
            if abs(daily_return) >= threshold_pct:
                return Alert(
                    fund_code=fund_code,
                    fund_name=fund_name,
                    alert_type="daily_change",
                    current_value=daily_return,
                    threshold=threshold_pct,
                    nav_date=nav_date,
                )
        except Exception as e:
            logger.error(f"[Watchdog] _check_single({fund_code}) 失败: {e}")

        return None

    def sync_and_check(self) -> list[Alert]:
        """
        同步自选基金最新净值 + 巡检（一站式）
        """
        watch_list = self.repo.get_watch_list()
        if not watch_list:
            return []

        all_alerts: list[Alert] = []
        logger.info(f"[Watchdog] 同步+巡检 {len(watch_list)} 只基金 ...")

        for item in watch_list:
            try:
                # 拉取最新净值
                df = self.datasource.fetch_nav_history(item.fund_code)
                if df.empty:
                    continue

                # 写入数据库
                self.repo.upsert_nav_records(item.fund_code, df.tail(30))

                # 检查最新一条
                latest = df.iloc[-1]
                daily_return = self._safe_float(latest.get("daily_return", 0))
                if abs(daily_return) >= item.alert_daily_pct:
                    alert = Alert(
                        fund_code=item.fund_code,
                        fund_name=item.fund_name,
                        alert_type="daily_change",
                        current_value=daily_return,
                        threshold=item.alert_daily_pct,
                        nav_date=str(latest.get("nav_date", "")),
                    )
                    all_alerts.append(alert)
                    logger.warning(str(alert))

            except Exception as e:
                logger.error(f"[Watchdog] sync {item.fund_code} 失败: {e}")

        self.alerts.extend(all_alerts)
        return all_alerts

    def start(self, interval_minutes: int = 30):
        """启动定时巡检"""
        self.scheduler.add_job(
            self.sync_and_check,
            trigger="interval",
            minutes=interval_minutes,
            id="watchdog_check",
            name="fund watchdog",
            next_run_time=datetime.now(),  # 立即执行一次
        )
        self.scheduler.start()
        logger.info(f"[Watchdog] 定时巡检已启动 (间隔={interval_minutes}分钟)")

    def stop(self):
        """停止定时巡检"""
        self.scheduler.shutdown(wait=False)
        logger.info("[Watchdog] 已停止")

    def get_recent_alerts(self, n: int = 50) -> list[Alert]:
        """获取最近 N 条告警"""
        return self.alerts[-n:]

    @staticmethod
    def _safe_float(val) -> float:
        try:
            return float(val)
        except (ValueError, TypeError):
            return 0.0
