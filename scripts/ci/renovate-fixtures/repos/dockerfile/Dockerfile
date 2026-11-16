FROM fixture/base:3.1.0 AS build
RUN true

FROM scratch
COPY --from=build /etc/hostname /hostname
