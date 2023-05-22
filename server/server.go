/**
  @author: Bruce
  @since: 2023/3/17
  @desc: //服务器核心
**/

package server

import (
	"fmt"
	"gts/iface"
	"gts/utils"
	"net"
	"os"
	"os/signal"
)

//Server 接口实现，定义一个Server服务类
type Server struct {
	//服务器的名称
	Name string
	//tcp4 or other
	IPVersion string
	//服务绑定的IP地址
	IP string
	//服务绑定的端口
	Port int
	//当前Server的消息管理模块，用来绑定MsgId和对应的处理方法
	msgHandler iface.IMsgHandle
	//当前Server的链接管理器
	ConnMgr iface.IConnManager

	//该Server的连接创建时Hook函数
	onConnStart func(conn iface.IConnection)
	//该Server的连接断开时的Hook函数
	onConnStop func(conn iface.IConnection)

	//心跳检测器
	hb iface.IHeartbeat

	// 捕获链接关闭状态
	exitChan chan struct{}
}

// NewServer 创建一个服务器句柄
func NewServer() iface.IServer {
	s := &Server{
		Name:       utils.Conf.Name,
		IPVersion:  utils.Conf.IpVersion,
		IP:         utils.Conf.Ip,
		Port:       utils.Conf.Port,
		msgHandler: NewMsgHandle(),
		ConnMgr:    NewConnManager(),
	}

	return s
}

//============== 实现 iface.IServer 里的全部接口方法 ========

func (s *Server) Start() {
	sf := utils.NewSnowflakeGenerator(utils.Conf.WorkerId, utils.Conf.DatacenterId)
	fmt.Printf("[START] Server listener at IP: %s, Port %d, is starting\n", s.IP, s.Port)
	s.exitChan = make(chan struct{})
	//开启一个go去做服务端Listener业务
	go func() {
		//0 启动worker工作池机制
		s.msgHandler.StartWorkerPool()

		//1 获取一个TCP的Addr
		addr, err := net.ResolveTCPAddr(s.IPVersion, fmt.Sprintf("%s:%d", s.IP, s.Port))
		if err != nil {
			fmt.Println("resolve tcp addr err: ", err)
			return
		}

		//2 监听服务器地址
		listener, err := net.ListenTCP(s.IPVersion, addr)
		if err != nil {
			fmt.Println("listen", s.IPVersion, "err", err)
			return
		}

		//已经监听成功
		fmt.Println("start Gts server  ", s.Name, " success, now listening...")
		//3 启动server网络连接业务
		go func() {
			for {
				//服务器最大连接控制,如果超过最大连接，那么则关闭此新的连接
				if s.ConnMgr.Len() >= utils.Conf.MaxConn {
					fmt.Println("Exceeded the maxConn")
					continue
				}
				//阻塞等待客户端建立连接请求
				conn, err := listener.AcceptTCP()
				if err != nil {
					fmt.Println("Accept err ", err)
					continue
				}

				//3.3 处理该新连接请求的 业务 方法， 此时应该有 handler 和 conn是绑定的
				cid, err := sf.NextVal()
				if err != nil {
					fmt.Println("Id gen err ", err)
					continue
				}
				dealConn := newServerConn(s, conn, cid)

				//HeartBeat 心跳检测
				if s.hb != nil {
					//从Server端克隆一个心跳检测器
					heartBeat := s.hb.Clone()
					//绑定当前链接
					heartBeat.BindConn(dealConn)
				}

				//3.4 启动当前链接的处理业务
				go dealConn.Start()
			}
		}()
		select {
		case <-s.exitChan:
			err := listener.Close()
			if err != nil {
				fmt.Println("listener close err: ", err)
			}
		}
	}()

}

func (s *Server) Stop() {
	fmt.Println("[STOP] Gts server , name ", s.Name)
	s.ConnMgr.ClearConn()
	s.exitChan <- struct{}{}
	close(s.exitChan)
}

func (s *Server) Serve() {
	s.Start()

	// 阻塞,否则主Go退出， listener的go将会退出
	c := make(chan os.Signal, 1)
	// 监听指定信号 ctrl+c kill信号
	signal.Notify(c, os.Interrupt, os.Kill)
	sig := <-c
	fmt.Printf("[SERVE] %s, Serve Interrupt, signal = %v", s.Name, sig)
}

// AddRouter 路由功能：给当前服务注册一个路由业务方法，供客户端链接处理使用
func (s *Server) AddRouter(msgId uint32, router iface.IRouter) {
	s.msgHandler.AddRouter(msgId, router)

	fmt.Println("Add Router success! ")
}

func (s *Server) GetConnMgr() iface.IConnManager {
	return s.ConnMgr
}
func (s *Server) GetMsgHandler() iface.IMsgHandle {
	return s.msgHandler
}

// SetOnConnStart 设置该Server的连接创建时Hook函数
func (s *Server) SetOnConnStart(hookFunc func(iface.IConnection)) {
	s.onConnStart = hookFunc
}

// SetOnConnStop 设置该Server的连接断开时的Hook函数
func (s *Server) SetOnConnStop(hookFunc func(iface.IConnection)) {
	s.onConnStop = hookFunc
}

// GetOnConnStart 得到该Server的连接创建时Hook函数
func (s *Server) GetOnConnStart() func(iface.IConnection) {
	return s.onConnStart
}

// GetOnConnStop 得到该Server的连接断开时的Hook函数
func (s *Server) GetOnConnStop() func(iface.IConnection) {
	return s.onConnStop
}
func (s *Server) GetHeartBeat() iface.IHeartbeat {
	return s.hb
}

// StartHeartBeat 启动心跳检测
func (s *Server) StartHeartBeat() {
	hb := NewHeartbeat(utils.Conf.GetHeartbeatInterval())
	s.AddRouter(hb.GetMsgID(), hb.GetRouter())
	s.hb = hb
}
