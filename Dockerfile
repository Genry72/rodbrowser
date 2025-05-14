FROM golang as builder

# Устанавливаем рабочую директорию
WORKDIR /src

# Копирование модуля зависимостей и загрузка необходимых библиотек
COPY go.mod .
COPY go.sum .
RUN go mod download

# Копирование всех исходных файлов и внутренних пакетов
COPY . .

# Собираем бинарник
RUN CGO_ENABLED=0 GOOS=linux go build -o server .

FROM ubuntu:22.04

WORKDIR /src

ARG apt_sources="http://archive.ubuntu.com"

#RUN sed -i "s|http://archive.ubuntu.com|$apt_sources|g" /etc/apt/sources.list && \
RUN apt-get update > /dev/null && \
    apt-get install --no-install-recommends -y \
    wget \
    libcanberra-gtk-module \
    libcanberra-gtk3-module \
    # chromium dependencies
    libnss3 \
    libxss1 \
    libasound2 \
    libxtst6 \
    libgtk-3-0 \
    libgbm1 \
    ca-certificates \
    # fonts
    fonts-liberation fonts-noto-color-emoji fonts-noto-cjk \
    # timezone
    tzdata \
    # process reaper
    dumb-init \
    # headful mode support, for example: $ xvfb-run chromium-browser --remote-debugging-port=9222
    xvfb && \
#    wget https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb && \
#    apt-get install -y ./google-chrome-stable_current_amd64.deb && \
    # cleanup \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    rm -rf /tmp/* /var/tmp/* && \
    rm -rf ./google-chrome-stable_current_amd64.deb

# Открываем порт сервера
EXPOSE 8081

# Откуда что куда
COPY --from=builder /src/server .

# Запуск с ключем
CMD ["/src/server", "-docker"]