package live

import "crypto/tls"

func tlsPair(cert, key string) (tls.Certificate, error) {
	return tls.X509KeyPair([]byte(cert), []byte(key))
}
