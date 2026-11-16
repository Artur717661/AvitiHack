FROM node:24-bookworm-slim AS frontend
WORKDIR /src
RUN npm install --global pnpm@11.25.0
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm build

FROM golang:1.27.4-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY data/ data/
RUN CGO_ENABLED=0 go build -trimpath -o /out/buddy .

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=backend /out/buddy /app/buddy
COPY --from=frontend /src/dist /app/web/dist
USER 65534:65534
ENV BUDDY_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/app/buddy"]
