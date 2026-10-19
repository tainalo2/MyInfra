FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./internal/ui/dist
ARG VERSION=dev
ARG IMAGE=
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION} -X main.image=${IMAGE}" -o /out/myinfra ./cmd/myinfra

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/myinfra /myinfra
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/myinfra"]
