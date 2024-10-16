package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"github.com/Genry72/rodbrowser/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/google/uuid"
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
	flagPstxID           = "pstxID"
)

type connectMap struct {
	m  map[string]string // Мапа для хранения папки с данными браузера
	mx sync.Mutex
}

// getNewName возвращает не занятое имя и добавляет его в занятые
func (c *connectMap) getNewName(pstxID string) string {
	c.mx.Lock()
	defer c.mx.Unlock()
	for i := 1; i < 10; i++ {
		n := fmt.Sprintf("%s%d", relativeUserDataPath, i)
		if _, ok := c.m[n]; !ok {
			c.m[n] = pstxID
			return n
		}
	}

	return ""
}

// deleteByName Освобождение занятого имени
func (c *connectMap) deleteByName(name string) {
	c.mx.Lock()
	delete(c.m, name)
	c.mx.Unlock()
}

// deleteByName Освобождение занятого имени по pstxID
func (c *connectMap) deleteByPstxID(pstxID string) {
	c.mx.Lock()
	defer c.mx.Unlock()
	for k, v := range c.m {
		if v == pstxID {
			delete(c.m, k)

			return
		}
	}

}

// checkNameAndPstxID Проверка, есть ли переданное имя в мапе и его соответствие с pstxID
func (c *connectMap) checkNameAndPstxID(name, pstxID string) error {
	c.mx.Lock()
	defer c.mx.Unlock()

	existPstxID, ok := c.m[name]
	if !ok {
		return fmt.Errorf("%s задан вручную", flags.UserDataDir)
	}

	if existPstxID != pstxID {
		return fmt.Errorf("%s not correct", flagPstxID)
	}

	return nil
}

func main() {
	zaplogger := logger.NewZapLogger("info", false)

	// Ограничение на максимальное количество одновременно запущенных браузеров
	limitchan := make(chan struct{}, 2)

	// Мапа для хранения "user-data-dir". Каждое новое подключение это отдельная папка (максимум 2)
	umap := &connectMap{
		m: make(map[string]string),
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
		pstxID := uuid.New().String()
		userDataPath := umap.getNewName(pstxID)
		zaplogger.Info("Выдали",
			zap.String("userDataPath", userDataPath),
			zap.Int("count open browsers", len(umap.m)),
			zap.String(flagPstxID, pstxID),
		)
		return getLounch(userDataPath, pstxID)
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

			// Получаем папку пользователя и pstxid из хедеров
			userDataPath, pstxID, err := getFolderNameAndPstxIDFromHeaders(c)
			if err != nil {
				zaplogger.Error("getFolderNameAndPstxIDFromHeaders", zap.Error(err))
				_ = c.AbortWithError(500, fmt.Errorf("getFolderNameFromHeadersЖ %w", err))
				return
			}

			// После отключения пользователя удаляем информацию с его подключением из мапы
			// Данные о параметрах подключения приходят во входящем запросе
			defer func() {
				umap.deleteByName(userDataPath)
				umap.deleteByPstxID(pstxID)
				zaplogger.Info("Закрыли",
					zap.String("userDataPath", userDataPath),
					zap.Int("count open browsers", len(umap.m)),
					zap.String(flagPstxID, pstxID),
				)
				<-limitchan
			}()

			// Проверка корректности передачи хедеров
			if err := umap.checkNameAndPstxID(userDataPath, pstxID); err != nil {
				zaplogger.Error("umap.checkNameAndPstxID", zap.Error(err), zap.String(flagPstxID, pstxID))
				_ = c.AbortWithError(400, fmt.Errorf("getFolderNameFromHeadersЖ %w", err))
				return
			}

			// Копирование папки сессий браузера, если их нет
			//if err := createOrMakeFolder(userDataPath); err != nil {
			//	zaplogger.Error("createOrMakeFolder", zap.Error(err))
			//	_ = c.AbortWithError(500, fmt.Errorf("createOrMakeFolder %w", err))
			//	return
			//}

			zaplogger.Info("Открыли",
				zap.Int("count open browsers", len(umap.m)),
				zap.String("userDataPath", userDataPath),
				zap.String(flagPstxID, pstxID),
			)

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

// getFolderNameAndPstxIDFromHeaders получение папки пользователя и pstx запроса из хедеров
func getFolderNameAndPstxIDFromHeaders(c *gin.Context) (userDataPath, pstxID string, err error) {
	l := &launcher.Launcher{}

	options := c.GetHeader(launcher.HeaderName)
	if options == "" {
		return "", "", fmt.Errorf("%s нет в хедерах", launcher.HeaderName)
	}

	if err := json.Unmarshal([]byte(options), l); err != nil {
		return "", "", fmt.Errorf("не распарсили %s: %w", options, err)
	}

	if len(l.Flags[flags.UserDataDir]) == 0 {
		return "", "", fmt.Errorf("нет папки пользователя в хедерах")
	}

	if len(l.Flags[flagPstxID]) == 0 {
		return "", "", fmt.Errorf("нет pstxID в хедерах")
	}

	userDataPath = l.Flags[flags.UserDataDir][0]

	pstxID = l.Flags[flagPstxID][0]

	return userDataPath, pstxID, nil
}

// getLounch возвращаем лаунчер, добавляем psxID в хедеры
func getLounch(userDataPath, pstxID string) *launcher.Launcher {
	path, _ := launcher.LookPath()

	l := launcher.New()
	for k := range l.Flags {
		l.Delete(k)
	}
	l.Bin(path)

	l.UserDataDir(userDataPath)

	for k, v := range launcher.NewUserMode().Flags {
		l.Set(k, v...)
	}

	// Закрытие браузера после отключения клиента
	l.Leakless(true)
	l.Set("no-first-run")
	l.Set(flagPstxID, pstxID)

	return l
}

//go:embed data/templateUserDataDir
var srcTemplateDir embed.FS

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

	if err := cp.Copy("data/templateUserDataDir", resultPath, cp.Options{
		FS:                srcTemplateDir,
		PermissionControl: cp.AddPermission(0777),
	}); err != nil {
		return fmt.Errorf("cp.Copy: %w", err)
	}

	return nil
}
