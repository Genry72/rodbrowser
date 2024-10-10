package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/go-rod/rod/lib/launcher"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

const hostport = ":8081"

func main() {
	mx := sync.Mutex{}

	m := launcher.NewManager()

	m.Logger = log.New(os.Stdout, "", 0)
	m.Defaults = func(writer http.ResponseWriter, request *http.Request) *launcher.Launcher {
		return getLounch()
	}
	// Снимаем ограничения на передачу заголовков. Все проверки можно добавить в gin
	m.BeforeLaunch = func(_ *launcher.Launcher, _ http.ResponseWriter, request *http.Request) {}

	gin.SetMode(gin.ReleaseMode)
	g := gin.New()

	g.GET("/", func(c *gin.Context) {
		var u bool
		// Одно подключение за раз
		mx.Lock()
		defer mx.Unlock()
		if c.Request.Header.Get("Upgrade") == "websocket" {
			// Открытие браузера
			u = true
			fmt.Println("открыли")
			// За 1 минуту клиент должен выполнить всю работу и отключиться. Иначе принудительно закроем соединение
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
		} else {
			// Запросы на получение настроек. Это первый запрос, браузер не открывает
			fmt.Println("подключение")
		}

		m.ServeHTTP(c.Writer, c.Request)
		if u {
			// Прльзователь отключился или закрыл соединение
			fmt.Println("закрыли")
		}
	})

	fmt.Println("[rod-manager] listening on:", hostport)

	srv := &http.Server{
		Addr:    hostport,
		Handler: g,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func getLounch() *launcher.Launcher {
	l := launcher.NewUserMode()
	// Закрытие браузера после отключения клиента
	l.Leakless(true)

	return l
}
