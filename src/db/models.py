"""
SQLAlchemy ORM 模型定义

三张核心表：
- Fund:        基金基础信息（静态/半静态）
- NavRecord:   净值记录（时间序列）
- WatchItem:   自选基金列表
"""

from datetime import date, datetime

from sqlalchemy import (
    Column, String, Float, Integer, Date, DateTime, Boolean, Text, UniqueConstraint,
)
from sqlalchemy.orm import DeclarativeBase


class Base(DeclarativeBase):
    pass


class Fund(Base):
    """基金基础信息表"""

    __tablename__ = "fund"

    id = Column(Integer, primary_key=True, autoincrement=True)
    code = Column(String(16), unique=True, nullable=False, index=True, comment="基金代码")
    name = Column(String(128), nullable=False, comment="基金简称")
    fund_type = Column(String(32), comment="基金类型")
    company = Column(String(128), comment="基金公司")
    establish_date = Column(Date, comment="成立日期")
    scale_yuan = Column(Float, comment="规模（元）")

    # 最新净值快照
    latest_nav = Column(Float, comment="最新单位净值")
    accum_nav = Column(Float, comment="最新累计净值")
    daily_return = Column(Float, comment="最新日增长率（%）")
    nav_date = Column(Date, comment="净值日期")

    # 最近一年收益（供快速筛选）
    return_1m = Column(Float, comment="近1月收益率（%）")
    return_3m = Column(Float, comment="近3月收益率（%）")
    return_6m = Column(Float, comment="近6月收益率（%）")
    return_1y = Column(Float, comment="近1年收益率（%）")
    return_2y = Column(Float, comment="近2年收益率（%）")
    return_3y = Column(Float, comment="近3年收益率（%）")

    # 风控指标
    max_drawdown = Column(Float, comment="近1年最大回撤")
    sharpe_ratio = Column(Float, comment="夏普比率（近似）")

    # 元数据
    synced_at = Column(DateTime, default=datetime.now, comment="最后同步时间")
    is_active = Column(Boolean, default=True, comment="是否仍在运作")

    def __repr__(self):
        return f"<Fund(code={self.code}, name={self.name})>"


class NavRecord(Base):
    """基金净值历史记录表"""

    __tablename__ = "nav_record"

    id = Column(Integer, primary_key=True, autoincrement=True)
    fund_code = Column(String(16), nullable=False, index=True, comment="基金代码")
    nav_date = Column(Date, nullable=False, comment="净值日期")
    unit_nav = Column(Float, nullable=False, comment="单位净值")
    accum_nav = Column(Float, comment="累计净值")
    daily_return = Column(Float, comment="日增长率（%）")

    __table_args__ = (
        UniqueConstraint("fund_code", "nav_date", name="uq_fund_date"),
    )

    def __repr__(self):
        return f"<NavRecord(fund={self.fund_code}, date={self.nav_date})>"


class WatchItem(Base):
    """自选基金监控表"""

    __tablename__ = "watch_item"

    id = Column(Integer, primary_key=True, autoincrement=True)
    fund_code = Column(String(16), nullable=False, index=True, comment="基金代码")
    fund_name = Column(String(128), comment="基金名称")
    added_at = Column(DateTime, default=datetime.now, comment="添加时间")
    alert_enabled = Column(Boolean, default=True, comment="是否开启告警")
    alert_daily_pct = Column(Float, default=2.0, comment="日涨跌告警阈值（%）")
    notes = Column(Text, comment="备注")

    def __repr__(self):
        return f"<WatchItem(fund={self.fund_code})>"
