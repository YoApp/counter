.PHONY: run push

build:
	fig build

run: build
	fig up

push:
	docker build -t justyo/counter .
	docker push justyo/counter
