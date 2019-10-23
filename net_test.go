package helpers

import (
	"testing"
	"log"
)

func TestGetIntranetIP(t *testing.T) {
	ipArr, err := GetIntranetIP()
	if err != nil {
		log.Fatal("获取内网IP失败，error:", err.Error())
	}
	log.Printf("内网IP列表为：%+v", ipArr)
}
