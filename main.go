package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Genry72/rodbrowser/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// Ключ для запуска в докере
var isDocker bool

const hostport = ":8081"

const (
	// Относительный путь до папки с данными браузера
	relativeUserDataPath = "./data/"
)

func main() {
	flag.BoolVar(&isDocker, "docker", false, "true, если запускается в докере")

	flag.Parse()

	zaplogger := logger.NewZapLogger("info", false)

	m := launcher.NewManager()

	m.Logger = log.New(os.Stdout, "", 0)

	// Ограничение на максимальное количество одновременно запущенных браузеров
	limitchan := make(chan struct{}, 2)

	// Снимаем ограничения на передачу заголовков. Все проверки можно добавить в gin
	m.BeforeLaunch = func(l *launcher.Launcher, writer http.ResponseWriter, request *http.Request) {
		udd := l.Get(flags.UserDataDir)
		// Пользователь не передал путь до папки с данными браузера
		if udd == "" {
			udd = uuid.New().String()
			// Удаляем папку пользователя после закрытия браузера
			l.Delete(flags.KeepUserDataDir)
		}

		if !strings.HasPrefix(udd, relativeUserDataPath) {
			udd = relativeUserDataPath + udd
		}

		l.Set(flags.UserDataDir, udd)

		// Закрытие браузера после отключения клиента
		l.Leakless(true)

		// Для докера ставим обязательные значения
		if isDocker {
			l.Headless(true)
			l.Set("disable-gpu")
			l.Set("disable-dev-shm-usage")
			l.Set(flags.NoSandbox)
		}

		zaplogger.Info(l.Get(flags.UserDataDir))
	}

	// Сюда приходит первый запрос, для получения параметров подключения.
	m.Defaults = func(writer http.ResponseWriter, request *http.Request) *launcher.Launcher {
		limitchan <- struct{}{}

		zaplogger.Info("Первый запрос") //zap.Int("count open browsers", len(umap.m)),

		l := getEmptyLounch()

		return l
	}

	gin.SetMode(gin.ReleaseMode)

	g := gin.New()

	// Все запросы обрабатываем в gin перед передачей в менеджер go-rod
	g.GET("/", func(c *gin.Context) {
		// Открытие браузера. Здесь передаются данные которые мы выдали при первом подключении
		if c.Request.Header.Get("Upgrade") == "websocket" {
			// За 1 час клиент должен выполнить всю работу и отключиться. Иначе принудительно закроем соединение
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
			defer cancel()

			c.Request = c.Request.WithContext(ctx)

			l, err := getLouncherByHeaders(c.GetHeader(launcher.HeaderName))
			if err != nil {
				zaplogger.Error("getLouncherByHeaders", zap.Error(err))
				_ = c.AbortWithError(500, fmt.Errorf("getLouncherByHeaders %w", err))
				return
			}

			userDataPath := l.Get(flags.UserDataDir)

			// После отключения пользователя удаляем информацию с его подключением из мапы
			// Данные о параметрах подключения приходят во входящем запросе
			defer func() {
				zaplogger.Info("Закрыли",
					zap.String("userDataPath", userDataPath),
					//zap.Int("count open browsers", len(umap.m)),
				)
				<-limitchan
			}()

			zaplogger.Info("Открыли")

		}

		// Передаем управление менеджеру
		m.ServeHTTP(c.Writer, c.Request)
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

func getLouncherByHeaders(lanchHeader string) (*launcher.Launcher, error) {
	if lanchHeader == "" {
		return nil, fmt.Errorf("пустой хедер")
	}

	l := &launcher.Launcher{}

	if err := json.Unmarshal([]byte(lanchHeader), l); err != nil {
		return nil, fmt.Errorf("не распарсили %s: %w", lanchHeader, err)
	}

	return l, nil
}

// getLounch возвращаем лаунчер, добавляем psxID в хедеры
func getEmptyLounch() *launcher.Launcher {
	path, _ := launcher.LookPath()

	l := launcher.New()
	for k := range l.Flags {
		l.Delete(k)
	}

	l.Bin(path)

	for k, v := range launcher.NewUserMode().Flags {
		l.Set(k, v...)
	}

	// Закрытие браузера после отключения клиента
	l.Leakless(true)
	l.Set("no-first-run")

	return l
}
