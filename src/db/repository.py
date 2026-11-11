"""
数据访问层（Repository 模式）

封装 SQLAlchemy 会话管理，提供对 Fund / NavRecord / WatchItem 的 CRUD 操作。
上层业务代码不应直接操作 Session，通过此模块访问数据库。
"""

from contextlib import contextmanager
from datetime import date, datetime
from typing import Generator, Optional

import pandas as pd
from loguru import logger
from sqlalchemy import create_engine, func
from sqlalchemy.orm import Session, sessionmaker

from src.db.models import Base, Fund, NavRecord, WatchItem


class Repository:
    """统一数据访问入口"""

    def __init__(self, db_url: str = "sqlite:///funds.db"):
        self.engine = create_engine(db_url, echo=False)
        self.SessionLocal = sessionmaker(bind=self.engine)
        # 自动建表
        Base.metadata.create_all(self.engine)
        logger.info(f"[DB] 已连接: {db_url}")

    @contextmanager
    def _session(self) -> Generator[Session, None, None]:
        """获取会话的上下文管理器"""
        session = self.SessionLocal()
        try:
            yield session
            session.commit()
        except Exception:
            session.rollback()
            raise
        finally:
            session.close()

    # ==========================================
    # Fund CRUD
    # ==========================================

    def upsert_funds(self, df: pd.DataFrame) -> int:
        """
        批量 upsert 基金基础数据

        Parameters
        ----------
        df : pd.DataFrame
            包含 code, name, fund_type, unit_nav, daily_return 等列

        Returns
        -------
        int : 写入的基金数量
        """
        with self._session() as session:
            count = 0
            for _, row in df.iterrows():
                code = str(row.get("code", row.get("基金代码", "")))
                if not code:
                    continue

                fund = session.query(Fund).filter(Fund.code == code).first()
                if fund is None:
                    fund = Fund(code=code)
                    session.add(fund)

                # 映射字段（兼容 AkShare 中文列名和英文列名）
                fund.name = self._get(row, "name", "基金简称") or fund.name
                fund.fund_type = self._get(row, "fund_type", "基金类型") or fund.fund_type
                fund.latest_nav = self._get(row, "unit_nav", "单位净值") or fund.latest_nav
                fund.accum_nav = self._get(row, "accum_nav", "累计净值") or fund.accum_nav
                fund.daily_return = self._get(row, "daily_return", "日增长率") or fund.daily_return
                # 阶段收益
                fund.return_1m = self._get(row, "return_1m", "近1月") or fund.return_1m
                fund.return_3m = self._get(row, "return_3m", "近3月") or fund.return_3m
                fund.return_6m = self._get(row, "return_6m", "近6月") or fund.return_6m
                fund.return_1y = self._get(row, "return_1y", "近1年") or fund.return_1y
                fund.return_2y = self._get(row, "return_2y", "近2年") or fund.return_2y
                fund.return_3y = self._get(row, "return_3y", "近3年") or fund.return_3y
                # 净值日期
                nav_date = self._datetime(row, "nav_date", "日期")
                if nav_date:
                    fund.nav_date = nav_date
                fund.synced_at = datetime.now()
                count += 1

            logger.info(f"[DB] upsert_funds: {count} 条")
            return count

    def get_fund(self, code: str) -> Optional[Fund]:
        """按代码查询单只基金"""
        with self._session() as session:
            return session.query(Fund).filter(Fund.code == code).first()

    def get_all_funds(
        self,
        fund_type: Optional[str] = None,
        min_scale: Optional[float] = None,
        limit: int = 500,
    ) -> list[Fund]:
        """查询基金列表（支持按类型和规模筛选）"""
        with self._session() as session:
            q = session.query(Fund).filter(Fund.is_active == True)
            if fund_type:
                q = q.filter(Fund.fund_type == fund_type)
            if min_scale:
                q = q.filter(Fund.scale_yuan >= min_scale * 1e8)
            return q.limit(limit).all()

    # ==========================================
    # NavRecord CRUD
    # ==========================================

    def upsert_nav_records(self, fund_code: str, df: pd.DataFrame) -> int:
        """批量 upsert 净值记录"""
        with self._session() as session:
            count = 0
            for _, row in df.iterrows():
                nd = self._datetime(row, "nav_date", "净值日期")
                un = self._get(row, "unit_nav", "单位净值")
                if nd is None or un is None:
                    continue

                rec = (
                    session.query(NavRecord)
                    .filter(NavRecord.fund_code == fund_code, NavRecord.nav_date == nd)
                    .first()
                )
                if rec is None:
                    rec = NavRecord(fund_code=fund_code, nav_date=nd)
                    session.add(rec)

                rec.unit_nav = float(un)
                rec.accum_nav = self._get(row, "accum_nav", "累计净值") or rec.accum_nav
                rec.daily_return = self._get(row, "daily_return", "日增长率") or rec.daily_return
                count += 1

            logger.info(f"[DB] upsert_nav: {fund_code} — {count} 条")
            return count

    def get_nav_history(self, fund_code: str, days: int = 365) -> pd.DataFrame:
        """获取基金近期净值历史"""
        with self._session() as session:
            records = (
                session.query(NavRecord)
                .filter(NavRecord.fund_code == fund_code)
                .order_by(NavRecord.nav_date.desc())
                .limit(days)
                .all()
            )
            if not records:
                return pd.DataFrame()
            return pd.DataFrame(
                [
                    {
                        "nav_date": r.nav_date,
                        "unit_nav": r.unit_nav,
                        "accum_nav": r.accum_nav,
                        "daily_return": r.daily_return,
                    }
                    for r in records
                ]
            )

    # ==========================================
    # WatchItem CRUD
    # ==========================================

    def add_to_watch(self, fund_code: str, fund_name: str = "", notes: str = "") -> WatchItem:
        """添加基金到自选列表"""
        with self._session() as session:
            existing = session.query(WatchItem).filter(WatchItem.fund_code == fund_code).first()
            if existing:
                logger.info(f"[DB] {fund_code} 已在自选中")
                return existing
            item = WatchItem(fund_code=fund_code, fund_name=fund_name, notes=notes)
            session.add(item)
            logger.info(f"[DB] 已添加自选: {fund_code} {fund_name}")
            return item

    def remove_from_watch(self, fund_code: str) -> bool:
        """从自选列表中移除基金"""
        with self._session() as session:
            item = session.query(WatchItem).filter(WatchItem.fund_code == fund_code).first()
            if item:
                session.delete(item)
                logger.info(f"[DB] 已移除自选: {fund_code}")
                return True
            return False

    def get_watch_list(self) -> list[WatchItem]:
        """获取全部自选基金"""
        with self._session() as session:
            return session.query(WatchItem).all()

    # ==========================================
    # 元数据
    # ==========================================

    def get_last_sync_time(self):
        """获取数据库最后一次同步时间"""
        with self._session() as session:
            result = session.query(func.max(Fund.synced_at)).scalar()
            return result

    # ==========================================
    # DataFrame 导出
    # ==========================================

    def get_funds_df(
        self,
        fund_type: Optional[str] = None,
        min_scale: Optional[float] = None,
        limit: int = 50000,
    ) -> pd.DataFrame:
        """
        将 Fund 表导出为 DataFrame（供筛选引擎使用）

        使用原生 SQL 直接查询，跳过 ORM 对象序列化开销。
        """
        import pandas as pd

        cols = [
            "code", "name", "fund_type",
            "latest_nav", "accum_nav", "daily_return",
            "return_1m", "return_3m", "return_6m", "return_1y",
            "return_2y", "return_3y",
            "max_drawdown", "sharpe_ratio", "scale_yuan",
        ]

        with self.engine.connect() as conn:
            # Build SQL manually for speed
            where = ["is_active = 1"]
            if fund_type:
                where.append(f"fund_type = '{fund_type}'")
            if min_scale:
                where.append(f"scale_yuan >= {min_scale * 1e8}")

            sql = (
                f"SELECT {', '.join(cols)} FROM fund "
                f"WHERE {' AND '.join(where)} "
                f"LIMIT {limit}"
            )
            df = pd.read_sql_query(sql, conn)

        # Rename for compatibility
        col_rename = {
            "latest_nav": "unit_nav",
        }
        df = df.rename(columns={k: v for k, v in col_rename.items() if k in df.columns})
        return df

    # ==========================================
    # Helpers
    # ==========================================

    @staticmethod
    def _get(row, *keys):
        """从行中获取第一个非空值（支持多键 fallback）"""
        for k in keys:
            v = row.get(k)
            if v is not None and pd.notna(v):
                return v
        return None

    @staticmethod
    def _datetime(row, *keys):
        """从行中获取并转为 date 类型"""
        v = Repository._get(row, *keys)
        if v is None:
            return None
        if isinstance(v, (date, datetime)):
            return v.date() if isinstance(v, datetime) else v
        try:
            return pd.Timestamp(v).date()
        except Exception:
            return None
