#.PHONY: rebuild
rebuild:
	docker-compose down
	docker-compose up --build
	#docker-compose up -d --build