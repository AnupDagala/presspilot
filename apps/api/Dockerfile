FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /api .
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /api /api
ENV API_BIND=0.0.0.0 PORT=8080
EXPOSE 8080
ENTRYPOINT ["/api"]
