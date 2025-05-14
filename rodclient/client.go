package rodclient

import (
	"context"
	"fmt"
	"github.com/Genry72/rodbrowser/pkg/logger"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"go.uber.org/zap"
	"sync"
)

type Browser struct {
	remote           bool
	hostport         string
	browser          *rod.Browser
	connectCount     int
	fnModifyLouncher func(launcher *launcher.Launcher) // для изменения параметров запуска браузера
	mx               sync.Mutex
	log              *zap.Logger
}

/*
New Создание нового клиента
Пример:

	fnModifyLouncher := func(l *launcher.Launcher) {
		l.Set("disable-http2")
		l.Headless(false)
		l.UserDataDir("datadir")
		//l.KeepUserDataDir()
	}
	client := rodclient.New(fnModifyLouncher, zaplogger).Remote("localhost:8081")
*/
func New(fnModifyLouncher func(launcher *launcher.Launcher)) *Browser {
	return &Browser{
		fnModifyLouncher: fnModifyLouncher,
		log:              logger.NewZapLogger("info", false)}
}

// Remote Подключение к удаленному хосту
func (r *Browser) Remote(hostport string) *Browser {
	r.mx.Lock()
	defer r.mx.Unlock()
	r.remote = true
	r.hostport = hostport

	return r
}

// Connect Подключение к браузеру.
// Нужно выполнить Browser.Disconnect() после использования
func (r *Browser) Connect(ctx context.Context) (*rod.Browser, error) {
	r.mx.Lock()
	defer r.mx.Unlock()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("проверка контекста перед подключением: %w", ctx.Err())
	default:
	}

	defer func() {
		r.log.Info("connectCount", zap.Int("count", r.connectCount))
	}()

	if r.isConnected() {
		r.connectCount++
		return r.browser, nil
	}

	var (
		br  *rod.Browser
		err error
	)

	if r.remote {
		br, err = getRemoteBrowser(r.hostport, r.fnModifyLouncher)
	} else {
		br, err = getLocalBrowser(r.fnModifyLouncher)
	}

	if err != nil {
		return nil, fmt.Errorf("getBrowser: %w", err)
	}

	if err := br.Connect(); err != nil {
		return nil, fmt.Errorf("br.Connect: %w", err)
	}

	br.WithPanic(func(fail interface{}) {
		r.log.Error("err", zap.Any("", fail))
	})

	r.browser = br
	r.connectCount++

	r.log.Info("open")

	return r.browser, nil
}

// Disconnect Закрытие браузера, если нет активных подключений
func (r *Browser) Disconnect() {
	r.mx.Lock()
	defer r.mx.Unlock()
	defer func() {
		r.log.Info("connectCount", zap.Int("count", r.connectCount))
	}()

	if !r.isConnected() {
		r.connectCount = 0
		return
	}

	r.connectCount--

	if r.connectCount == 0 {
		if err := r.browser.Close(); err != nil {
			r.log.Error(err.Error())
		}

		r.log.Info("close")
	}
}

// isConnected Возвращает true если есть активное подключение
func (r *Browser) isConnected() bool {
	if r.browser == nil {
		return false
	}

	if _, err := r.browser.Pages(); err != nil {
		return false
	}
	return true
}

func getRemoteBrowser(hostport string, fnModifyLouncher func(launcher *launcher.Launcher)) (*rod.Browser, error) {
	l, err := launcher.NewManaged("ws://" + hostport)
	if err != nil {
		return nil, fmt.Errorf("launcher.NewManaged: %w", err)
	}

	if fnModifyLouncher != nil {
		fnModifyLouncher(l)
	}

	client, err := l.Client()
	if err != nil {
		return nil, fmt.Errorf("l.Client: %w", err)
	}

	br := rod.New().Client(client)

	return br, nil
}

func getLocalBrowser(fnModifyLouncher func(launcher *launcher.Launcher)) (*rod.Browser, error) {
	l := launcher.NewUserMode()

	l.Leakless(true)

	if fnModifyLouncher != nil {
		fnModifyLouncher(l)
	}

	l.Set("no-first-run")

	launch, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("l.Launch: %w", err)
	}

	br := rod.New().ControlURL(launch)

	return br, nil
}
