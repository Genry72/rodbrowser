#.PHONY: rebuild
rebuild:
	docker-compose down
	docker-compose up -d --build

#.PHONY: start
start:
	docker-compose start

#.PHONY: stop
stop:
	docker-compose stop