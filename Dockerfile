FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app
COPY --from=build /out/api /app/api
COPY migrations /app/migrations
ENV PORT=8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
