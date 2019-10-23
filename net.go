package helpers

import (
	"net"
	"fmt"
)

/**
 * 功能：获取本机内网IP
 * 参数：无
 * 返回值：[]string, error
 */
func GetIntranetIP() ([]string, error) {
	addrArr, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var ipArr []string
	for _, address := range addrArr {
		if ipNet, ok := address.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				fmt.Println("ip:", ipNet.IP.String())
				ipArr = append(ipArr, ipNet.IP.String())
			}
		}
	}
	return ipArr, nil
}
