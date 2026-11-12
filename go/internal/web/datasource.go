package web

// 数据源看板 API：
//   GET  /api/datasources        数据源清单 + 能力矩阵 + 当前路由 + 连通状态
//   POST /api/datasources/test   测试连接：空 body 全测，{"id":x} 单测
//   GET  /api/datasources/routing        当前功能路由
//   POST /api/datasources/routing       更新路由 {routing:{feature:source}}（校验+持久化+热生效）

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"killer/internal/source"
	"killer/internal/store"
)

// 状态缓存：页面打开即显状态，按钮强制刷新。
var (
	statusMu    sync.Mutex
	statusCache = map[string]source.ProbeResult{}
	statusAt    time.Time
)

const statusTTL = 60 * time.Second

// getStatus 返回连通状态（过期或缺失则探测一次）。
func getStatus(r *http.Request, force bool) map[string]source.ProbeResult {
	statusMu.Lock()
	stale := time.Since(statusAt) > statusTTL || len(statusCache) < len(source.Sources)
	statusMu.Unlock()
	if !force && !stale {
		statusMu.Lock()
		defer statusMu.Unlock()
		out := make(map[string]source.ProbeResult, len(statusCache))
		for k, v := range statusCache {
			out[k] = v
		}
		return out
	}
	res := source.ProbeAll(r.Context())
	statusMu.Lock()
	statusCache = res
	statusAt = time.Now()
	statusMu.Unlock()
	return res
}

// handleDataSources GET /api/datasources
func (s *Server) handleDataSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.dataSourcePayload(getStatus(r, false)))
}

// dataSourcePayload 组装看板完整数据（GET 与测试共用同一结构）。
func (s *Server) dataSourcePayload(status map[string]source.ProbeResult) map[string]any {
	return map[string]any{
		"sources":  source.Sources,
		"features": source.Features,
		"routing":  source.RoutingSnapshot(),
		"status":   status,
	}
}

// handleDataSourceTest POST /api/datasources/test — 测试连接按钮。
//   - 无 body（或空 body）：探测全部数据源
//   - {"id":"eastmoney"}：只探测指定数据源，其余状态保持不变
// 两种情况都返回与 GET 相同的完整结构，前端可直接整体替换渲染。
func (s *Server) handleDataSourceTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if raw, err := io.ReadAll(io.LimitReader(r.Body, 4096)); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &body) // 非法 JSON 按全测处理
	}

	if body.ID != "" {
		// 校验数据源存在
		found := false
		for _, src := range source.Sources {
			if src.ID == body.ID {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "未知数据源: "+body.ID, http.StatusBadRequest)
			return
		}
		res := source.ProbeOne(r.Context(), body.ID)
		statusMu.Lock()
		statusCache[body.ID] = res
		statusMu.Unlock()
		statusMu.Lock()
		cached := make(map[string]source.ProbeResult, len(statusCache))
		for k, v := range statusCache {
			cached[k] = v
		}
		statusMu.Unlock()
		writeJSON(w, s.dataSourcePayload(cached))
		return
	}
	writeJSON(w, s.dataSourcePayload(getStatus(r, true)))
}

// routingKey 持久化键。
const routingKey = "routing"

// handleRouting GET/POST /api/datasources/routing
func (s *Server) handleRouting(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, source.RoutingSnapshot())

	case http.MethodPost:
		var body struct {
			Routing map[string]string `json:"routing"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Routing) == 0 {
			http.Error(w, `invalid body: need {"routing":{"feature":"source"}}`, http.StatusBadRequest)
			return
		}
		// 校验：功能存在且数据源支持该功能
		for feat, srcID := range body.Routing {
			if !source.Supports(feat, srcID) {
				http.Error(w, "数据源 "+srcID+" 不支持功能 "+feat, http.StatusBadRequest)
				return
			}
		}
		source.SetRouting(body.Routing) // 热生效（内存）
		// 持久化（合并写入，保留未提及的功能）
		merged := source.RoutingSnapshot()
		if raw, err := s.st.GetSetting(routingKey); err == nil && raw != "" {
			var old map[string]string
			if json.Unmarshal([]byte(raw), &old) == nil {
				for k, v := range old {
					if _, ok := merged[k]; !ok {
						merged[k] = v
					}
				}
			}
		}
		if b, err := json.Marshal(merged); err == nil {
			if err := s.st.SetSetting(routingKey, string(b)); err != nil {
				log.Printf("[Web] 路由持久化失败: %v", err)
			}
		}
		writeJSON(w, map[string]any{"status": "ok", "routing": source.RoutingSnapshot()})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// LoadRouting 启动时从 DB 恢复路由设置（main 调用）。
func LoadRouting(st *store.Store) {
	raw, err := st.GetSetting(routingKey)
	if err != nil || raw == "" {
		return
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		log.Printf("[Web] 路由设置解析失败: %v", err)
		return
	}
	source.SetRouting(m)
	log.Printf("[Web] 已从数据库恢复数据源路由: %v", m)
}
