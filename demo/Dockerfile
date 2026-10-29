FROM golang:1.26.7-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /chinspector .

FROM scratch
COPY --from=build /chinspector /chinspector
ENTRYPOINT ["/chinspector"]
