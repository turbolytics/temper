FROM golang:1.23-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /counter .

FROM busybox:1.36
COPY --from=build /counter /counter
