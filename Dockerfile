FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm install
COPY frontend/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM golang:1.25-alpine AS iwaradl-builder
RUN apk add --no-cache git
WORKDIR /src
RUN git clone --depth 1 --branch v1.5.4 https://github.com/Izumiko/iwaradl.git .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/iwaradl .

FROM golang:1.25-alpine AS service-builder
WORKDIR /app
COPY backend/go.mod ./
RUN go mod download || true
COPY backend/ ./
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /out/service ./cmd/service
RUN mkdir -p /out/data /out/media /out/scratch

FROM gcr.io/distroless/static-debian12 AS runtime
WORKDIR /app
COPY --from=service-builder /out/data /data
COPY --from=service-builder /out/media /media
COPY --from=service-builder /out/scratch /scratch
COPY --from=service-builder /out/service /app/service
COPY --from=iwaradl-builder /out/iwaradl /app/iwaradl
COPY --from=frontend-builder /app/frontend/out /app/web
EXPOSE 8842
ENTRYPOINT ["/app/service"]
