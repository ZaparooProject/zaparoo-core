FROM arm64v8/debian:bookworm

RUN apt-get update -y && apt-get upgrade -y
RUN apt-get install ca-certificates openssl -y && update-ca-certificates
RUN apt-get install build-essential git curl -y && \
    apt-get install libusb-dev libtool autoconf automake pkgconf -y

COPY --from=golang:1.23 /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

RUN mkdir /internal

# install custom libnfc
RUN cd /internal && \
    git clone --depth 1 https://github.com/nfc-tools/libnfc.git
RUN cd /internal/libnfc && \
    autoreconf -vis && \
    ./configure --with-drivers=acr122_usb,pn532_i2c,pn532_uart && \
    make -j "$(nproc)" && \
    make install

# install task
RUN sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

# drop permissions on output files
ARG UID=1000
ARG GID=1000
RUN groupadd -f -g $GID build && useradd -m -u $UID -g $GID build
USER build

WORKDIR /build
RUN git config --global --add safe.directory /build
