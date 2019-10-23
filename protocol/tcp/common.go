package tcp

import (
	"net"
	"sync"
	"git-pd.megvii-inc.com/slg-service/dgw/components"
	"io"
)

type Protocol interface {
	GetHeadSize() (size uint64)
	GetBodySize(headBuffer []byte) (size uint64)
	CheckHead(buffer []byte) (err error)
	CheckBody(head []byte, body []byte) (err error)
	DecodeHead(headBuffer []byte) (head interface{})
	DecodeBody(buffer []byte) (body []byte)
	PackMsg(params []interface{}) (buffer []byte)
}


type Conn struct {
	Addr		string
	Conn		net.Conn
	Proto		Protocol
	Buffer		[]byte
	ReqMaxSize	uint64
	tcpLock		sync.Mutex
	DataPack	chan []byte
	ErrChan		chan error
	ErrorCount	uint8
}

/**
	函数名：RecDataPack
	功能描述：获取数据包
	参数1：buffer byte切片，存储从网络流中读取的数据
	返回值2：chan []byte，返回接受数据的管道
 */
func(conn *Conn) ReadData() {
	conn.DataPack 	= make(chan []byte)
	conn.ErrChan	= make(chan error)
	go conn.recDataPack()
}


/**
	函数名：recDataPack
	功能描述：获取数据包
 */
func(conn *Conn) recDataPack(){
	recDone 		:= false
	curReadHeadSize	:= uint64(0)
	curReadBodySize	:= uint64(0)
	conn.Buffer		= make([]byte, conn.ReqMaxSize)
	for ; !recDone ; {
		//1 获取请求报头
		readSize, err := conn.recMsgHead(curReadHeadSize)
		if err != nil {
			curReadHeadSize += readSize
			//1.1 HEAD_ERROR代表数据获取成功，但是报头不合法，需要重新获取报头，TODO:可以考虑是不是打个日志啥的
			if err == components.HeadError {
				continue
			}
			//1.2 连接关闭了，直接返回
			if err == io.EOF {
				conn.ErrChan <- err
				return
			}
			//1.3 其他的错误代表从网络流获取数据失败，如果连续失败3次直接返回错误
			conn.ErrorCount++
			if conn.ErrorCount > 3 {
				conn.ErrChan <- err
				return
			}
		}
		curReadHeadSize += readSize

		//2 获取请求Body
		readSize, err = conn.recMsgBody(conn.Buffer[0:curReadHeadSize], conn.Buffer[curReadHeadSize:], curReadBodySize)
		if err != nil {
			curReadBodySize += readSize
			//2.1 BODY_ERROR代表数据获取成功，但是Body不合法，需要重新获取报头，TODO:可以考虑是不是打个日志啥的
			if err == components.BodyError {
				continue
			}
			//2.2 连接关闭了，直接返回
			if err == io.EOF {
				conn.ErrChan <- err
				return
			}
			//2.3 其他的错误代表从网络流获取数据失败，如果连续失败3次直接返回错误
			conn.ErrorCount++
			if conn.ErrorCount > 3 {
				conn.ErrChan <- err
				return
			}
		}
		conn.ErrorCount = 0	//只要读成功一次，就清空计数器，有可能是网络抖了
		curReadBodySize += readSize
		recDone = true
	}
	conn.DataPack <- conn.Buffer[0:curReadHeadSize + curReadBodySize]
}

/**
	函数名：recMsgHead
	功能描述：获取请求报头，这个函数可重入，如果body获取失败，可以重新获取报头
	参数1：buffer byte切片，存储从网络流中读取的数据
	参数2：curReadSize uint64，代表当前包总共读取的字节数
	返回值1：size uint64，此次读取的字节数，读取失败时为0
	返回值2：err，返回读取错误
 */
func(conn *Conn) recMsgHead(curReadSize uint64) (size uint64, err error) {
	//1 获取报头长度，并初始化buffer大小
	headSize 	:= conn.Proto.GetHeadSize()

	//2 获取字节流，直到获取到headSize大小的数据。
	var thisTimeReadSize uint64 = 0
	for ; curReadSize < headSize ; {
		waitReadSize	:= headSize - curReadSize
		headBuffer		:= make([]byte, waitReadSize)
		readSize, err	:= conn.Conn.Read(headBuffer)
		if err != nil {
			return thisTimeReadSize, err
		}
		if readSize <= 0 {
			continue
		}
		copy(conn.Buffer[curReadSize:], headBuffer[:readSize])
		curReadSize 		+= uint64(readSize)
		thisTimeReadSize 	+= uint64(readSize)
	}

	//3 调用实现类的checkHead方法，校验头部是否完整
	checkErr		:= conn.Proto.CheckHead(conn.Buffer)
	if checkErr != nil {
		//3.1 去掉头部第一个字节
		index := uint64(0)
		for ; index < curReadSize - 1 ; index++ {
			conn.Buffer[index] = conn.Buffer[index + 1]
		}
		//3.2 再读入一个字节
		oneByteBuffer	:= make([]byte, 1)
		for {
			readSize, err 	:= conn.Conn.Read(oneByteBuffer)
			if err != nil {
				return thisTimeReadSize, err
			}
			if readSize < 1 {
				continue
			}
		}
		//3.3 把读入的字节接到之前的buffer后面
		conn.Buffer[index] = oneByteBuffer[0]
		return thisTimeReadSize, components.HeadError
	}
	return thisTimeReadSize, nil
}

/**
 * 函数名：recMsgBody
 * 功能描述：获取请求Body，这个函数可重入
 * 参数1：head byte切片，存储报头的内容，以便获取
 * 参数2：buffer byte切片，存储从网络流中读取的数据
 * 参数3：curReadSize uint64，代表当前包总共读取的字节数
 * 返回值1：size uint64，此次读取的字节数，读取失败时为0
 * 返回值2：err，返回读取错误
 */
func(conn *Conn) recMsgBody(head []byte, buffer []byte, curReadSize uint64) (size uint64, err error) {
	//1 从Head中获取Body的长度，并初始化buffer大小
	bodySize 	:= conn.Proto.GetBodySize(head)

	//2 获取字节流，直到获取到bodySize大小的数据。
	var thisTimeReadSize uint64 = 0
	for ; curReadSize < bodySize ; {
		waitReadSize	:= bodySize - curReadSize
		bodyBuffer		:= make([]byte, waitReadSize)
		readSize, err	:= conn.Conn.Read(bodyBuffer)
		if err != nil {
			return thisTimeReadSize, err
		}
		if readSize <= 0 {
			continue
		}
		copy(buffer[curReadSize:], bodyBuffer[:readSize])
		curReadSize 		+= uint64(readSize)
		thisTimeReadSize 	+= uint64(readSize)
	}

	//3 调用实现类的checkBody方法，校验Body是否完整
	checkErr	:= conn.Proto.CheckBody(head, buffer)
	if checkErr != nil {
		//3.1 去掉头部第一个字节
		index := uint64(0)
		for ; index < uint64(len(head)) + curReadSize - 1 ; index++ {
			conn.Buffer[index] = conn.Buffer[index + 1]
		}
		//3.2 再读入一个字节
		oneByteBuffer	:= make([]byte, 1)
		for {
			readSize, err 	:= conn.Conn.Read(oneByteBuffer)
			if err != nil {
				return thisTimeReadSize, err
			}
			if readSize < 1 {
				continue
			}
		}
		//3.3 把读入的字节接到之前的buffer后面
		conn.Buffer[index] = oneByteBuffer[0]
		return thisTimeReadSize, components.BodyError
	}
	return thisTimeReadSize, nil
}

/**
 * 函数名：sendMsg
 * 功能描述：将一段字节流发送到网络缓存
 * 参数1：buffer []byte 待发送的字节流
 * 返回值1：err，返回写入错误
 */
func (conn *Conn) sendMsg (buffer []byte) (err error){
	allSize		:= 0
	nowIndex	:= 0
	bufferSize 	:= len(buffer)
	for {
		currSize, err := conn.Conn.Write(buffer[nowIndex:])
		if err != nil {
			return err
		}
		allSize += currSize
		if allSize >= bufferSize {
			break
		}
		nowIndex = allSize
	}
	return nil
}

/**
 * 函数名：SendTcpMsg
 * 功能描述：发送一段内容给远程socket
 * 参数1：params 变长参数，根据不同协议，对参数进行打包
 * 返回值1：err，返回写入错误
 */
func(conn *Conn) SendTcpMsg(params ...interface{}) (err error) {
	buffer	:= conn.Proto.PackMsg(params)
	conn.tcpLock.Lock()
	err		= conn.sendMsg(buffer)
	conn.tcpLock.Unlock()
	return err
}

func(conn *Conn) Close() {
	conn.Conn.Close()
	conn.Buffer = nil
}