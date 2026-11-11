# 🔍 Fund Killer — 基金筛选与监控系统

基于 Python 的公募基金数据采集、智能筛选与异动监控系统。

## 功能概览

- **数据采集**: 通过 AkShare 拉取全市场开放式基金净值、持仓、基金经理等数据
- **多维度筛选**: 按收益率、最大回撤、夏普比率、规模等指标筛选优质基金
- **异动监控**: 定时监控自选基金，净值涨跌超阈值自动告警
- **可视化面板**: Streamlit Web 界面，支持筛选、对比、图表展示

## 快速开始

### 1. 安装依赖

```bash
pip install -r requirements.txt
```

### 2. 同步数据

```bash
python scripts/sync_data.py
```

### 3. 启动面板

```bash
streamlit run src/ui/dashboard.py
```

## 项目结构

```
killer/
├── config/settings.yaml      # 全局配置
├── docs/data-research.md     # 数据源调研
├── src/
│   ├── data/                 # 数据获取层
│   ├── db/                   # 数据库层
│   ├── screening/            # 筛选引擎
│   ├── monitoring/           # 监控引擎
│   └── ui/                   # Streamlit 面板
├── scripts/                  # 运行脚本
└── tests/                    # 测试
```

## 数据源

| 数据源 | 费用 | 说明 |
|--------|------|------|
| AkShare | 免费 | 主数据源，爬取天天基金网公开数据 |
| Tushare Pro | 免费200次/天 | 备用数据源，需注册 Token |
| Baostock | 免费 | 历史数据补充 |

## License

MIT
