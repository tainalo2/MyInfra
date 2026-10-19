package boot

import "net"

func requireLoopback(host string) error {
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errString("PostgreSQL local doit écouter sur une adresse de bouclage.")
	}
	return nil
}
