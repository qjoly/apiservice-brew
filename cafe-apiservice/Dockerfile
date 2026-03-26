FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o cafe-apiservice .

# Image finale minimaliste
FROM scratch

COPY --from=builder /app/cafe-apiservice /cafe-apiservice

EXPOSE 8443

ENTRYPOINT ["/cafe-apiservice"]
