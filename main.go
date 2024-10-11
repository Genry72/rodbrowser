package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Genry72/rodbrowser/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	cp "github.com/otiai10/copy"
	"go.uber.org/zap"
	"log"
	"net/http"
	"os"
	"path"
	"sync"
	"time"
)

const hostport = ":8081"

const (
	// Относительный путь до папки с данными браузера
	relativeUserDataPath = "./data/"
)

type connectMap struct {
	m  map[string]struct{} // Мапа для хранения папки с данными браузера
	mx sync.Mutex
}

// getNewName возвращает не занятое имя и добавляет его в занятые
func (c *connectMap) getNewName() string {
	c.mx.Lock()
	defer c.mx.Unlock()
	for i := 1; i < 10; i++ {
		n := fmt.Sprintf("%s%d", relativeUserDataPath, i)
		if _, ok := c.m[n]; !ok {
			c.m[n] = struct{}{}
			return n
		}
	}

	return ""
}

// deleteNewName Освобождение занятого имени
func (c *connectMap) deleteNewName(name string) {
	c.mx.Lock()
	delete(c.m, name)
	c.mx.Unlock()
}

func main() {
	zaplogger := logger.NewZapLogger("info", false)

	// Ограничение на максимальное количество одновременно запущенных браузеров
	limitchan := make(chan struct{}, 2)

	// Мапа для хранения "user-data-dir". Каждое новое подключение это отдельная папка (максимум 2)
	umap := &connectMap{
		m: make(map[string]struct{}),
	}

	m := launcher.NewManager()

	m.Logger = log.New(os.Stdout, "", 0)

	// Снимаем ограничения на передачу заголовков. Все проверки можно добавить в gin
	m.BeforeLaunch = func(l *launcher.Launcher, writer http.ResponseWriter, request *http.Request) {}
	// Сюда приходит первый запрос, для получения параметров подключения.
	// Здесь же выдается пользователю путь до директории хранения данных браузера
	// Данные забираются из контекста, добавленные gin-ом

	m.Defaults = func(writer http.ResponseWriter, request *http.Request) *launcher.Launcher {
		limitchan <- struct{}{}
		// Получаем свободную папку
		userDataPath := umap.getNewName()
		if err := createOrMakeFolder(userDataPath); err != nil {
			zaplogger.Error("createOrMakeFolder", zap.Error(err))
			writer.WriteHeader(500)
			return nil
		}

		return getLounch(userDataPath)
	}

	gin.SetMode(gin.ReleaseMode)

	g := gin.New()

	// Все запросы обрабатываем в gin перед передачей в менеджер go-rod
	g.GET("/", func(c *gin.Context) {
		// Открытие браузера. Здесь передаются данные которые мы выдали при первом подключении
		if c.Request.Header.Get("Upgrade") == "websocket" {
			// За 1 минуту клиент должен выполнить всю работу и отключиться. Иначе принудительно закроем соединение
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()

			c.Request = c.Request.WithContext(ctx)
			// После отключения пользователя удаляем информацию с его подключением из мапы
			// Данные о паремтрах подключения приходят во входящем запросе
			userDataPath, err := getFolderNameFromHeaders(c)
			if err != nil {
				zaplogger.Error("getFolderNameFromHeaders", zap.Error(err))
				_ = c.AbortWithError(500, fmt.Errorf("getFolderNameFromHeadersЖ %w", err))
				return
			}
			// После окончания запроса "выписываем" пользователя
			defer func() {
				fmt.Println("Закрыли")
				umap.deleteNewName(userDataPath)
				<-limitchan

			}()
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

// getFolderNameFromHeaders получение папки пользователя из хедеров
func getFolderNameFromHeaders(c *gin.Context) (string, error) {
	l := &launcher.Launcher{}

	options := c.GetHeader(launcher.HeaderName)
	if options == "" {
		return "", fmt.Errorf("%s нет в хедерах", launcher.HeaderName)
	}

	if err := json.Unmarshal([]byte(options), l); err != nil {
		return "", fmt.Errorf("не распарсили %s: %w", options, err)
	}

	if len(l.Flags[flags.UserDataDir]) == 0 {
		return "", fmt.Errorf("нет папки пользователя в хедерах")
	}

	userDataPath := l.Flags[flags.UserDataDir][0]

	return userDataPath, nil
}

func getLounch(userDataPath string) *launcher.Launcher {
	path, _ := launcher.LookPath()

	l := launcher.New()
	for k := range l.Flags {
		l.Delete(k)
	}
	l.Bin(path)

	l.UserDataDir(userDataPath)
	b := launcher.NewUserMode()
	_ = b
	for k, v := range launcher.NewUserMode().Flags {
		l.Set(k, v...)
	}

	// Закрытие браузера после отключения клиента
	l.Leakless(true)
	l.Set("no-first-run")

	return l
}

// Создание или копирование папки с сессиями браузера
func createOrMakeFolder(userDataPath string) error {
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}

	folder := path.Base(userDataPath)

	resultPath := path.Join(pwd, relativeUserDataPath, folder)
	_, err = os.Stat(resultPath)
	if err == nil {
		return nil
	}

	src := path.Join(pwd, relativeUserDataPath, "templateUserDataDir")

	if err := cp.Copy(src, resultPath); err != nil {
		return fmt.Errorf("cp.Copy: %w", err)
	}

	return nil
}
