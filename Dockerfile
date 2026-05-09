FROM golang:1.25.4-alpine AS builder

WORKDIR /app

COPY go.mod go.su[m] ./
RUN go mod download

COPY . .
RUN go build -o bin/backend .

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/bin/backend ./bin/backend

EXPOSE ${PORT}

CMD ["./bin/backend"]
