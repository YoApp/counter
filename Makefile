.PHONY: build push

build:
	docker build -t justyo/counter .

push: build
	docker push justyo/counter
