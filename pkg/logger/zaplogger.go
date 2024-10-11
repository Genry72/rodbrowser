package logger

import (
	"log"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Todo прикрутить на ошибки отправку в телегу.

// NewZapLogger создает новый экземпляр логгера типа *zap.Logger.
// Принимает аргумент level - уровень логирования в виде строки.
// Возвращает указатель на *zap.Logger.
//
// Аргументы:
// - level: строка, уровень логирования
//
// Пример использования:
// logger := NewZapLogger("info").
func NewZapLogger(level string, toFile bool) *zap.Logger {
	cfg := zap.NewProductionEncoderConfig()
	cfg.TimeKey = "time"
	cfg.EncodeDuration = zapcore.MillisDurationEncoder
	cfg.EncodeTime = zapcore.RFC3339TimeEncoder
	cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	lvl, err := zap.ParseAtomicLevel(level)

	if err != nil {
		log.Fatal(err)
	}

	out := os.Stdout

	if toFile {
		// Поставить, если не нужен цвет zapcore.LowercaseLevelEncoder
		cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder

		file, err := os.OpenFile("log.txt", os.O_APPEND|os.O_RDWR|os.O_CREATE, 0o666)
		if err != nil {
			log.Fatal(err)
		}

		out = file
	}

	consoleEncoder := zapcore.NewConsoleEncoder(cfg)

	core := zapcore.NewCore(consoleEncoder,
		zapcore.AddSync(out),
		lvl,
	)

	return zap.New(core).WithOptions(zap.AddCaller())
}
