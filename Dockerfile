FROM golang:1.26-alpine

WORKDIR /app

ADD . /app

RUN go build -o bussar .

FROM alpine:latest

COPY --from=0 /app/bussar /usr/local/bin/bussar

ENV CONFIG_PATH=config.yaml
CMD [ "bussar", "-config", "$CONFIG_PATH" ]
