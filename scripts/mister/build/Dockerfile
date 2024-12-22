FROM arm32v7/debian:bookworm-backports

RUN apt-get update -y && apt-get upgrade -y

# make go mod download work
RUN apt-get install -y ca-certificates openssl && \
    openssl s_client -showcerts -connect github.com:443 </dev/null 2>/dev/null|openssl x509 -outform PEM > /usr/local/share/ca-certificates/github.crt && \
    openssl s_client -showcerts -connect proxy.golang.org:443 </dev/null 2>/dev/null|openssl x509 -outform PEM >  /usr/local/share/ca-certificates/proxy.golang.crt && \
    update-ca-certificates

# install go and app dependencies
RUN apt-get install build-essential git curl wget ncurses-dev -y && \
    apt-get install golang-doc/bookworm-backports golang-go/bookworm-backports golang-src/bookworm-backports golang/bookworm-backports -y
RUN mkdir /internal

# install pcsc-lite and ccid dependencies
RUN apt-get install -y flex libusb-1.0-0-dev zlib1g-dev
# install custom version of pcsc-lite
RUN cd /internal && \
    wget https://pcsclite.apdu.fr/files/pcsc-lite-2.0.0.tar.bz2 && \
    tar xf pcsc-lite-2.0.0.tar.bz2 && \
    cd pcsc-lite-2.0.0 && \
    ./configure --disable-libsystemd --disable-libudev -enable-static && \
    make -j "$(nproc)" && \
    make install
# install custom version of ccid
RUN cd /internal && \
    wget https://ccid.apdu.fr/files/ccid-1.5.4.tar.bz2 && \
    tar xf ccid-1.5.4.tar.bz2 && \
    cd ccid-1.5.4 && \
    ./configure -enable-static && \
    make -j "$(nproc)" && \
    make install

# install libnfc dependencies
RUN apt-get install -y libusb-dev libtool autoconf automake
RUN cd /internal && \
    git clone --depth 1 https://github.com/nfc-tools/libnfc.git
# apply patches
COPY patches/pcsc-initiator_poll_target.patch /internal/libnfc/pcsc-initiator_poll_target.patch
COPY patches/uart_flock.patch /internal/libnfc/uart_flock.patch
RUN cd /internal/libnfc && \
    patch -p1 < pcsc-initiator_poll_target.patch && \
    patch -p1 < uart_flock.patch
# build and install libnfc
RUN cd /internal/libnfc && \
    autoreconf -vis && \
    ./configure --with-drivers=all && \
    make -j "$(nproc)" && \
    make install

# copy build script, not using task because go is too old in this environment
COPY build.sh /usr/local/bin/build.sh
RUN chmod +x /usr/local/bin/build.sh

# drop permissions on output files
ARG UID=1000 GID=1000
RUN groupadd -g $GID build && useradd -m -u $UID -g $GID build
USER build

WORKDIR /build

RUN git config --global --add safe.directory /build
