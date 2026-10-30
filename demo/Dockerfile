FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /ch_insp .

FROM scratch
COPY --from=build /ch_insp /ch_insp
ENTRYPOINT ["/ch_insp"]
