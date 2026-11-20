package pointed

// padLength is 32 on amd64 and 64 on arm64, so the counter after a Pad sits 32 bytes in on one and 64 on the other.
const padLength = 32
