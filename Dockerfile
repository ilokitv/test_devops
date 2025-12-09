
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod ./
COPY go.sum* ./
RUN go mod download

COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o weather-app .


FROM alpine:latest

RUN apk --no-cache add ca-certificates wget

WORKDIR /root/


COPY --from=builder /app/weather-app .

EXPOSE 8080

CMD ["./weather-app"]

