# syntax=docker/dockerfile:1
# Build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/server .

# Runtime stage — includes CA certs for the HTTPS APIs (Photon/OSRM/Duffel/Amadeus)
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/server /usr/local/bin/server
EXPOSE 8080
ENTRYPOINT ["server"]
