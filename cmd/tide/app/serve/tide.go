package serve

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ict/tide/pkg/clsstat"
	"github.com/ict/tide/pkg/cmnct"
	"github.com/ict/tide/pkg/containerx"
	"github.com/ict/tide/pkg/event"
	"github.com/ict/tide/pkg/interfaces"
	"github.com/ict/tide/pkg/procmgr"
	"github.com/ict/tide/pkg/routine"
	"github.com/ict/tide/pkg/server"
	"github.com/ict/tide/pkg/server/handlers/cloudhdlr"
	"github.com/ict/tide/pkg/server/handlers/clsstathdlr"
	"github.com/ict/tide/pkg/server/handlers/stubmgrhdlr"
	"github.com/ict/tide/pkg/stringx"
	"github.com/ict/tide/pkg/stubmgr"
	"github.com/ict/tide/proto/commpb"
	"github.com/ict/tide/proto/eventpb"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

var ServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start tide serve process",
	Long:  `Start tide serve process.`,
	Run: func(cmd *cobra.Command, args []string) {
		serveTide()
	},
}

var (
	outboundIP string
	port       int
	cpuNum     int
	addresses  string
	nodeName   string
	roleParam  string
)

func init() {
	ServeCmd.Flags().StringVar(&outboundIP, "outbound-ip", getOutboundIP(), "the outbound ip")
	ServeCmd.Flags().IntVar(&port, "port", 10001, "the server port")
	ServeCmd.Flags().IntVar(&cpuNum, "cpu-num", runtime.NumCPU(), "the cpu number can be used")
	ServeCmd.Flags().StringVar(&addresses, "addresses", "", "the other servers")
	ServeCmd.Flags().StringVar(&nodeName, "name", "", "the node name (exp)")
	ServeCmd.Flags().StringVar(&roleParam, "role", "things", "the role of this device")
}

func initEnv() {
	// go func() {
	// 	log.Println(http.ListenAndServe(":9998", nil))
	// }()

	var rLim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_MSGQUEUE, &rLim); err != nil {
		panic(err)
	}
	logrus.Info("Rlimit init: ", rLim)
	if err := unix.Setrlimit(unix.RLIMIT_MSGQUEUE, &unix.Rlimit{Cur: unix.RLIM_INFINITY, Max: unix.RLIM_INFINITY}); err != nil {
		panic(err)
	}
	if err := unix.Getrlimit(unix.RLIMIT_MSGQUEUE, &rLim); err != nil {
		panic(err)
	}
	logrus.Info("Rlimit final: ", rLim)

	// setup logger
	logrus.SetLevel(logrus.DebugLevel)
	// logrus.SetFormatter(&logrus.TextFormatter{
	// 	FullTimestamp:   true,
	// 	TimestampFormat: "2006-01-02T15:04:05.000000000",
	// })
	logrus.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000000000",
	})
	// logrus.SetFormatter(&nested.Formatter{
	// 	NoColors:        true,
	// 	TimestampFormat: "2006-01-02 15:04:05",
	// 	// 显示输出的文件和函数名, 便于定位
	// 	// CallerFirst:     true,
	// 	// CustomCallerFormatter: func(f *runtime.Frame) string {
	// 	// 	s := strings.Split(f.Function, ".")
	// 	// 	funcName := s[len(s)-1]
	// 	// 	return fmt.Sprintf(" [%s#%d][%s()]", path.Base(f.File), f.Line, funcName)
	// 	// },
	// })
	// logrus.SetReportCaller(true)

	// 若文件夹不存在则创建
	os.MkdirAll("/tmp/tide", 0777)
	logFile, err := os.OpenFile("/tmp/tide/tide.log", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		logrus.Fatal("open log file failed, ", err)
	}
	logrus.SetOutput(logFile)
}

func initFlags() *tideServeConfig {
	flag.Parse()
	cpuNum = runtime.NumCPU()
	runtime.GOMAXPROCS(cpuNum)

	selfRole := clsstat.ParseRole(roleParam)
	if selfRole == clsstat.UNKNOWN {
		logrus.Fatalf("unknown role type, `%s`", roleParam)
	}

	if selfRole == clsstat.CLOUD {
		cpuNum = 48
	} else if selfRole == clsstat.EDGE {
		// cpuNum = 6
	}

	// build self infomation
	selfId := stringx.GenerateId()
	if nodeName != "" {
		selfId = nodeName
	}
	selfIp := outboundIP
	selfAddr := stringx.Concat(selfIp, ":", strconv.FormatInt(int64(port), 10))
	// TODO: configure GPU Num
	selfRes := &routine.Resource{CpuNum: cpuNum, GpuNum: 4}

	logrus.Info("GOMAXPROCS: ", runtime.GOMAXPROCS(0))
	logrus.Info("SelfID:", selfId)
	logrus.Info("SelfAddr:", selfAddr)
	logrus.Info("SelfCpuNum:", cpuNum)
	logrus.Info("SelfRole:", selfRole)

	clsstat.Init(selfId, selfAddr, selfRole, selfRes)
	containerx.InitCgroup()
	if selfRole == clsstat.EDGE {
		containerx.InitCombCgroup()
	}

	return &tideServeConfig{
		OutboundAddr: selfAddr,
		Port:         port,
		CpuNum:       cpuNum,
		Addresses:    addresses,
		NodeName:     nodeName,
		SelfRole:     selfRole,
		SelfRes:      selfRes,
	}

}

func serveTide() {
	initEnv()
	config := initFlags()

	procM := procmgr.NewProcMgr(config.SelfRes)
	stubM := stubmgr.NewStubMgr(procM)

	// setup event server
	evtDispr := event.NewEventDispatcher()
	if config.SelfRole != clsstat.CLOUD {
		stubmgrhdlr.RegisterEventHandler(evtDispr, stubM)
		clsstathdlr.RegisterEventHandler(evtDispr)
	} else {
		cloudhdlr.RegisterEventHandler(evtDispr, stubM)
	}
	evtSvr := server.NewEventServer(evtDispr)

	cmnct.Init(config.NodeName, config.OutboundAddr, evtDispr)

	// setup grpc server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		logrus.Fatalf("failed to listen: %v", err)
	}
	grpcSvr := grpc.NewServer()
	eventpb.RegisterEventServiceServer(grpcSvr, evtSvr)

	// start daemon goroutine
	go func() {
		logrus.Infof("grpc server listening at %v", lis.Addr())
		if err := grpcSvr.Serve(lis); err != nil {
			logrus.Fatalf("failed to serve: %v", err)
		}
	}()
	// only EDGE node boradcast its resource state
	if config.SelfRole == clsstat.EDGE {
		procM.StartHeartbeat()
	}
	broadcastJoin(config.NodeName, config.OutboundAddr, config.SelfRole, config.SelfRes)

	// graceful exit
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, os.Interrupt)
	onErr := func(err error) {
		if err != nil {
			logrus.Error(err)
		}
	}

	exitCh := make(chan struct{})

	go func() {
		for {
			select {
			case sig := <-c:
				logrus.Infof("Got %s signal. Aborting...", sig)
				close(exitCh)
				return
			default:
				time.Sleep(10 * time.Second)
				logrus.Info("RUNNING and wait for exit signal")
			}
		}
	}()

	<-exitCh
	// sig := <-c
	// logrus.Infof("Got %s signal. Aborting...", sig)
	grpcSvr.GracefulStop()
	onErr(stubM.Close())
	onErr(procM.Close())
	containerx.DeleteAllCgroup()
	if config.SelfRole == clsstat.EDGE {
		containerx.DeleteAllCombCgroup()
	}
}

func broadcastJoin(selfId, selfAddr string, role clsstat.NodeRole, selfRes *routine.Resource) {
	servers := strings.Split(addresses, ",")

	ndNewEvtPb := &eventpb.NodeNewEvent{
		NodeId:        selfId,
		Address:       selfAddr,
		Role:          int32(role),
		TotalResource: &commpb.Resource{CpuNum: int32(selfRes.CpuNum), GpuNum: int32(selfRes.GpuNum)},
		IsReply:       false,
	}
	content, err := proto.Marshal(ndNewEvtPb)
	if err != nil {
		logrus.Fatal(err)
	}
	evt := &event.Event{Type: interfaces.NodeNewEvt, Content: content}
	for _, server := range servers {
		if server == "" {
			continue
		}
		if err := cmnct.Singleton().SendToAddress(server, evt); err != nil {
			logrus.Fatal(err)
		}
	}
}

// Get preferred outbound ip of this machine
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP.String()
}
