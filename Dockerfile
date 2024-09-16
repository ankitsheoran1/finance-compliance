FROM golang:1.23.0-alpine3.20
# Copy local code to the container image.
WORKDIR /app
RUN apk update && apk add --no-cache git

COPY . .

# Build the command inside the container.
RUN go build -o server .

EXPOSE 8080

# Run the web service on container startup.
CMD ["./server"]