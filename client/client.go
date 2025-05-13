package client

import (
	"context"
	"fmt"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"go.uber.org/zap"
	"sync"
)

type Browser struct {
	remote       bool
	hostport     string
	browser      *rod.Browser
	connectCount int
	connected    bool
	mx           sync.Mutex
	log          *zap.Logger
}

func New(log *zap.Logger) *Browser {
	return &Browser{log: log}
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
// Нужно выполнить browser.Close() после использования
func (r *Browser) Connect(ctx context.Context) (*rod.Browser, error) {
	r.mx.Lock()
	defer r.mx.Unlock()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("проверка контекста перед подключением: %w", ctx.Err())
	default:
	}

	defer func() {
		fmt.Println(r.connectCount)
	}()

	if r.connected && r.remote { // повторное подключение возможно только для удаленных вызовов
		if err := r.browser.Connect(); err != nil {
			r.connected = false
			r.connectCount = 0
		}
	}

	if r.connected {
		r.connectCount++
		return r.browser, nil
	}

	var (
		br  *rod.Browser
		err error
	)

	if r.remote {
		br, err = getRemoteBrowser(r.hostport)
	} else {
		br, err = getLocalBrowser()
	}

	if err != nil {
		return nil, err
	}

	if err := br.Connect(); err != nil {
		return nil, fmt.Errorf("br.Connect: %w", err)
	}

	br.WithPanic(func(i interface{}) {
		r.log.Error("err", zap.Any("", i))
	})

	r.browser = br
	r.connected = true
	r.connectCount++

	r.log.Info("open")

	return r.browser, nil
}

func getLocalBrowser() (*rod.Browser, error) {
	l := launcher.NewUserMode()

	l.Leakless(true)

	launch, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("l.Launch: %w", err)
	}

	br := rod.New().ControlURL(launch)

	return br, nil
}

func (r *Browser) Close() {
	r.mx.Lock()
	defer r.mx.Unlock()
	defer func() {
		fmt.Println(r.connectCount)
	}()

	if !r.connected {
		return
	}

	r.connectCount--

	if r.connectCount == 0 {
		if err := r.browser.Close(); err != nil {
			r.log.Error(err.Error())
		}

		r.connected = false

		r.log.Info("close")
	}
}

func getRemoteBrowser(hpstport string) (*rod.Browser, error) {
	l, err := launcher.NewManaged("ws://" + hpstport)
	if err != nil {
		return nil, fmt.Errorf("launcher.NewManaged: %w", err)
	}

	l.Set("disable-http2")

	//l.Headless(true) // не влияет при запуске на удаленном сервере

	l.KeepUserDataDir()

	client, err := l.Client()
	if err != nil {
		return nil, fmt.Errorf("l.Client: %w", err)
	}

	br := rod.New().Client(client)

	return br, nil
}
