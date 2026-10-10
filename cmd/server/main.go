// 这是程序的入口：打开数据库，然后开始等浏览器来访问。
// 在项目根目录运行：go run ./cmd/server
// 然后打开 http://localhost:8080
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"qiutu/internal/db"
	"qiutu/internal/httpapi"
	"qiutu/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	dbPath := flag.String("db", "data/qiutu.db", "数据库文件")
	webDir := flag.String("web", "web", "页面目录")
	flag.Parse()

	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	server := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(store.New(database), *webDir).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	shown := *addr
	if strings.HasPrefix(shown, ":") {
		shown = "localhost" + shown
	}
	log.Printf("康复自重训练已启动，用浏览器打开 http://%s", shown)
	log.Fatal(server.ListenAndServe())
}
