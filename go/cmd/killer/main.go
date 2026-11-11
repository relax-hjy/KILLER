// killer — 大盘监控系统 KILLER（Go 重写版）
//
// 原系统 Fund Killer 是基金筛选+监控；本版本用 Go 重写，
// 核心功能改为对股票大盘（上证/深证/创业板等指数）的实时监控与阈值告警。
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"killer/internal/config"
	"killer/internal/monitor"
	"killer/internal/store"
	"killer/internal/web"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("[Main] 配置加载完成: %s (指数 %d 个)", *cfgPath, len(cfg.Indices))

	st, err := store.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()
	log.Printf("[Main] 数据库: %s", cfg.Database.Path)

	wd, err := monitor.New(cfg, st)
	if err != nil {
		log.Fatalf("初始化监控失败: %v", err)
	}
	wd.Start()
	defer wd.Stop()

	srv, err := web.New(cfg, wd, st)
	if err != nil {
		log.Fatalf("初始化 Web 服务失败: %v", err)
	}

	// Ctrl+C 优雅退出（直接退出，defer 无需执行）
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-done
		log.Println("[Main] 收到退出信号，停止服务 ...")
		os.Exit(0)
	}()

	if err := srv.Serve(); err != nil {
		log.Fatalf("Web 服务异常退出: %v", err)
	}
}
