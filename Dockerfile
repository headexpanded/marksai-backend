FROM golang:1.24.4-alpine3.22

# Install git and curl
RUN apk add --no-cache git curl

WORKDIR /app

# Copy project files
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build main.go as an executable
RUN go build -o marksai-server .

# Expose PocketBase default port
EXPOSE 8090

CMD ["./marksai-server", "serve", "--http=0.0.0.0:8090", "--dir", "/pb_data"]
