FROM golang:1.23.2-bookworm AS build

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

RUN go mod download && go mod verify

COPY main.go ./

RUN go build -o /intro

FROM ubuntu:22.10

COPY --from=build /intro /intro

ENTRYPOINT [ "/intro" ]