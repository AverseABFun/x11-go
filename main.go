package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

const (
	PROTO_VER_MAJOR uint16 = 11
	PROTO_VER_MINOR uint16 = 0
)

type Request struct {
	MajorOp uint8
	Length  uint16
	Data0   uint8
	Data    []byte
}

type Reply struct {
	Length uint32
	Data   []byte
}

type ErrorReport struct {
	Data [32]byte
}

func OpenSocket(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

type Endianness bool

func (e Endianness) String() string {
	if e == LITTLE_ENDIAN {
		return "Little Endian"
	} else {
		return "Big Endian"
	}
}

const (
	LITTLE_ENDIAN Endianness = false
	BIG_ENDIAN    Endianness = true
)

var endianness Endianness = true

func uint16ToBytes(num uint16) []byte {
	var out = []byte{}
	if endianness {
		out = binary.BigEndian.AppendUint16([]byte{}, num)
	} else {
		out = binary.LittleEndian.AppendUint16([]byte{}, num)
	}
	return out
}

func bytesToUint16(bytes []byte) uint16 {
	var out uint16
	if endianness {
		out = binary.BigEndian.Uint16(bytes)
	} else {
		out = binary.LittleEndian.Uint16(bytes)
	}
	return out
}

func bytesToUint32(bytes []byte) uint32 {
	var out uint32
	if endianness {
		out = binary.BigEndian.Uint32(bytes)
	} else {
		out = binary.LittleEndian.Uint32(bytes)
	}
	return out
}

func bytesToUint8(bytes []byte) uint8 {
	return bytes[0]
}

func readBytes(socket io.Reader, num uint) []byte {
	var out = make([]byte, num)
	var _, err = socket.Read(out)
	panicIfBad(err, fmt.Sprintf("%d: ", num))
	return out
}

func panicIfBad(err error, extra string) {
	if err != nil {
		panic(errors.New(extra + err.Error()))
	}
}

func writeToSock(sock net.Conn, data []byte) {
	var _, err = sock.Write(data)
	panicIfBad(err, "")
}

func skipBytes(socket io.Reader, num uint) {
	readBytes(socket, num)
}

func StartConn(socket net.Conn, auth XAuthority) Connection {
	endianness = false
	writeToSock(socket, []byte{0x6c, 0x00})
	writeToSock(socket, uint16ToBytes(PROTO_VER_MAJOR))
	writeToSock(socket, uint16ToBytes(PROTO_VER_MINOR))
	writeToSock(socket, uint16ToBytes(auth.NameLen))
	writeToSock(socket, uint16ToBytes(auth.DataLen))
	writeToSock(socket, []byte{0x00, 0x00})

	writeToSock(socket, []byte(auth.Name))
	var paddingName = 4 - (auth.NameLen % 4)
	if paddingName == 4 {
		paddingName = 0
	}
	writeToSock(socket, make([]byte, paddingName))

	writeToSock(socket, []byte(auth.Data))
	var paddingData = 4 - (auth.DataLen % 4)
	if paddingData == 4 {
		paddingData = 0
	}
	writeToSock(socket, make([]byte, paddingData))

	var status = readBytes(socket, 1)[0]
	if status == 0 {
		fmt.Print("Connection failed! ")
		var lenReason = readBytes(socket, 1)[0]
		var protoMajor = bytesToUint16(readBytes(socket, 2))
		var protoMinor = bytesToUint16(readBytes(socket, 2))
		fmt.Printf("Expected version %d.%d, and got version %d.%d! ", protoMajor, protoMinor, PROTO_VER_MAJOR, PROTO_VER_MINOR)
		skipBytes(socket, 2)
		var reason = string(readBytes(socket, uint(lenReason)))
		fmt.Printf("Got failed reason: %s", reason)
		return Connection{Errored: true, ErrorReason: reason}
	} else if status == 1 {
		fmt.Println("Connection succeeded!")
		skipBytes(socket, 1)
		var conn = Connection{}
		conn.Authority = auth
		var protoMajor = bytesToUint16(readBytes(socket, 2))
		var protoMinor = bytesToUint16(readBytes(socket, 2))
		conn.ServerVersion.Major = protoMajor
		conn.ServerVersion.Minor = protoMinor

		var extraDataLength = bytesToUint16(readBytes(socket, 2))

		var releaseNumber = bytesToUint32(readBytes(socket, 4))
		conn.Release = releaseNumber

		var resourceIdBase = bytesToUint32(readBytes(socket, 4))
		var resourceIdMask = bytesToUint32(readBytes(socket, 4))
		conn.ResourceIDBase = resourceIdBase
		conn.ResourceIDMask = resourceIdMask

		var motionBufferSize = bytesToUint32(readBytes(socket, 4))
		conn.MotionBufferSize = motionBufferSize

		var vendorLength = bytesToUint16(readBytes(socket, 2))
		conn.VendorLength = vendorLength

		var maxRequestLength = bytesToUint16(readBytes(socket, 2))
		conn.MaxRequestLength = maxRequestLength

		var numScreens = bytesToUint8(readBytes(socket, 1))
		conn.NumScreens = numScreens

		var numFormats = bytesToUint8(readBytes(socket, 1))
		conn.NumFormats = numFormats

		var imageByteOrder = Endianness(bytesToUint8(readBytes(socket, 1)) != 0)
		conn.ImageByteOrder = imageByteOrder

		var bitmapFormatBitOrder = Endianness(bytesToUint8(readBytes(socket, 1)) != 0)
		conn.BitmapFormatBitOrder = bitmapFormatBitOrder
		var bitmapFormatScanlineUnit = bytesToUint8(readBytes(socket, 1))
		conn.BitmapFormatScanlineUnit = bitmapFormatScanlineUnit
		var bitmapFormatScanlinePad = bytesToUint8(readBytes(socket, 1))
		conn.BitmapFormatScanlinePad = bitmapFormatScanlinePad

		var minKeycode = bytesToUint8(readBytes(socket, 1))
		conn.KeycodeRange.Min = Keycode(minKeycode)
		var maxKeycode = bytesToUint8(readBytes(socket, 1))
		conn.KeycodeRange.Max = Keycode(maxKeycode)

		skipBytes(socket, 4)
		var vendor = string(readBytes(socket, uint(vendorLength)))
		conn.Vendor = vendor
		var vendorPadding = 4 - (vendorLength % 4)
		if vendorPadding == 4 {
			vendorPadding = 0
		}
		skipBytes(socket, uint(vendorPadding))

		extraDataLength -= 8
		extraDataLength -= (vendorLength + vendorPadding + uint16(numScreens)) / 4
		extraDataLength /= 8

		var formats = []PixmapFormat{}
		for i := 0; i < int(extraDataLength); i++ {
			var newFormat = PixmapFormat{}
			newFormat.Depth = bytesToUint8(readBytes(socket, 1))
			newFormat.BitsPerPixel = bytesToUint8(readBytes(socket, 1))
			newFormat.ScanlinePad = bytesToUint8(readBytes(socket, 1))
			skipBytes(socket, 5)
			formats = append(formats, newFormat)
		}
		conn.PixmapFormats = formats

		var screens = []Screen{}
		for i := 0; i < int(numScreens); i++ {
			var newScreen = Screen{}
			newScreen.Root = Window(bytesToUint32(readBytes(socket, 4)))
			newScreen.DefaultColormap = Colormap(bytesToUint32(readBytes(socket, 4)))
			newScreen.WhitePixel = bytesToUint32(readBytes(socket, 4))
			newScreen.BlackPixel = bytesToUint32(readBytes(socket, 4))
			newScreen.CurrentInputMask = SetOfEvent(bytesToUint32(readBytes(socket, 4)))
			newScreen.WidthPix = bytesToUint16(readBytes(socket, 2))
			newScreen.HeightPix = bytesToUint16(readBytes(socket, 2))
			newScreen.WidthMM = bytesToUint16(readBytes(socket, 2))
			newScreen.HeightMM = bytesToUint16(readBytes(socket, 2))
			newScreen.InstalledMapsRange.Min = bytesToUint16(readBytes(socket, 2))
			newScreen.InstalledMapsRange.Max = bytesToUint16(readBytes(socket, 2))
			newScreen.RootVisual = VisualID(bytesToUint32(readBytes(socket, 4)))
			newScreen.BackingStores = BackingStores(bytesToUint8(readBytes(socket, 1)))
			newScreen.SaveUnders = bytesToUint8(readBytes(socket, 1)) != 0
			newScreen.RootDepth = bytesToUint8(readBytes(socket, 1))
			newScreen.AllowedDepthsLength = bytesToUint8(readBytes(socket, 1))
			for i := 0; i < int(newScreen.AllowedDepthsLength); i++ {
				var newDepth = Depth{}
				newDepth.Depth = bytesToUint8(readBytes(socket, 1))
				skipBytes(socket, 1)
				newDepth.VisualTypesLength = bytesToUint16(readBytes(socket, 2))
				skipBytes(socket, 4)
				for i := 0; i < int(newDepth.VisualTypesLength); i++ {
					var newVisualType = VisualType{}
					newVisualType.VisualID = VisualID(bytesToUint32(readBytes(socket, 4)))
					newVisualType.Class = VisualTypeClass(bytesToUint8(readBytes(socket, 1)))
					newVisualType.BitsPerRGBValue = bytesToUint8(readBytes(socket, 1))
					newVisualType.ColormapEntries = bytesToUint16(readBytes(socket, 2))
					newVisualType.RedMask = bytesToUint32(readBytes(socket, 4))
					newVisualType.GreenMask = bytesToUint32(readBytes(socket, 4))
					newVisualType.BlueMask = bytesToUint32(readBytes(socket, 4))
					skipBytes(socket, 4)
					newDepth.VisualTypes = append(newDepth.VisualTypes, newVisualType)
				}
				newScreen.AllowedDepths = append(newScreen.AllowedDepths, newDepth)
			}
			screens = append(screens, newScreen)
		}
		conn.Screens = screens
		return conn
	}
	return Connection{Errored: true, ErrorReason: fmt.Sprintf("Got unknown status of %d", status)}
}

type Version struct {
	Major uint16
	Minor uint16
}

type Range struct {
	Min int
	Max int
}

func (r Range) String() string {
	return fmt.Sprintf("%d-%d", r.Min, r.Max)
}

type Uint16Range struct {
	Min uint16
	Max uint16
}

func (r Uint16Range) String() string {
	return fmt.Sprintf("%d-%d", r.Min, r.Max)
}

type Keycode uint8

type KeycodeRange struct {
	Min Keycode
	Max Keycode
}

func (r KeycodeRange) String() string {
	return fmt.Sprintf("%d-%d", r.Min, r.Max)
}

type PixmapFormat struct {
	Depth        uint8
	BitsPerPixel uint8
	ScanlinePad  uint8
}

type Window uint32
type Colormap uint32
type Event uint32
type VisualID uint32
type BackingStores uint8
type VisualTypeClass uint8

const (
	VisualTypeClassStaticGray = VisualTypeClass(iota)
	VisualTypeClassStaticGrayScale
	VisualTypeClassStaticColor
	VisualTypeClassPseudoColor
	VisualTypeClassTrueColor
	VisualTypeClassDirectColor
)

const (
	BackingStoresNever = BackingStores(iota)
	BackingStoresWhenMapped
	BackingStoresAlways
)

type SetOfEvent uint32

func (set SetOfEvent) HasEvent(e Event) bool {
	return ((set >> e) & 0x1) != 0
}

func (set *SetOfEvent) SetEvent(e Event) {
	*set = SetOfEvent(*set | (1 << e))
}

func (set *SetOfEvent) UnsetEvent(e Event) {
	*set = SetOfEvent(*set & ^(1 << e))
}

const (
	EventKeyPress = Event(iota)
	EventKeyRelease
	EventButtonPress
	EventButtonRelease
	EventEnterWindow
	EventLeaveWindow
	EventPointerMotion
	EventPointerMotionHint
	EventButton1Motion
	EventButton2Motion
	EventButton3Motion
	EventButton4Motion
	EventButton5Motion
	EventButtonMotion
	EventKeymapState
	EventExposure
	EventVisibilityChange
	EventStructureNotify
	EventResizeRedirect
	EventSubstructureNotify
	EventSubstructureRedirect
	EventFocusChange
	EventPropertyChange
	EventColormapChange
	EventOwnerGrabButton
	EventUnused = Event(31)
)

type VisualType struct {
	VisualID        VisualID
	Class           VisualTypeClass
	BitsPerRGBValue uint8
	ColormapEntries uint16
	RedMask         uint32
	GreenMask       uint32
	BlueMask        uint32
}

type Depth struct {
	Depth             uint8
	VisualTypesLength uint16
	VisualTypes       []VisualType
}

type Screen struct {
	Root                Window
	DefaultColormap     Colormap
	WhitePixel          uint32
	BlackPixel          uint32
	CurrentInputMask    SetOfEvent
	WidthPix            uint16
	HeightPix           uint16
	WidthMM             uint16
	HeightMM            uint16
	InstalledMapsRange  Uint16Range
	RootVisual          VisualID
	BackingStores       BackingStores
	SaveUnders          bool
	RootDepth           uint8
	AllowedDepthsLength uint8
	AllowedDepths       []Depth
}

type Connection struct {
	Errored                  bool
	ErrorReason              string
	Authority                XAuthority
	ServerVersion            Version
	Release                  uint32
	ResourceIDBase           uint32
	ResourceIDMask           uint32
	MotionBufferSize         uint32
	VendorLength             uint16
	MaxRequestLength         uint16
	NumScreens               uint8
	NumFormats               uint8
	ImageByteOrder           Endianness
	BitmapFormatBitOrder     Endianness
	BitmapFormatScanlineUnit uint8
	BitmapFormatScanlinePad  uint8
	KeycodeRange             KeycodeRange
	Vendor                   string
	PixmapFormats            []PixmapFormat
	Screens                  []Screen
}

func GetSocketLocation() string {
	display := os.Getenv("DISPLAY")
	if display == "" {
		panic("DISPLAY environment variable is not set")
	}

	parts := strings.Split(display, ":")
	if len(parts) < 2 {
		panic("Invalid DISPLAY format")
	}

	displayNum := strings.Split(parts[1], ".")[0]

	return "/tmp/.X11-unix/X" + displayNum
}

func GetXauthorityFile() string {
	return os.Getenv("XAUTHORITY")
}

func ParseXauthorityFile(path string) XAuthority {
	endianness = true
	var file, err = os.Open(path)
	panicIfBad(err, "")
	var out = XAuthority{}
	out.Family = bytesToUint16(readBytes(file, 2))

	out.AddrLen = bytesToUint16(readBytes(file, 2))
	out.Address = string(readBytes(file, uint(out.AddrLen)))

	out.DispLen = bytesToUint16(readBytes(file, 2))
	out.Display = string(readBytes(file, uint(out.DispLen)))

	out.NameLen = bytesToUint16(readBytes(file, 2))
	out.Name = string(readBytes(file, uint(out.NameLen)))

	out.DataLen = bytesToUint16(readBytes(file, 2))
	out.Data = string(readBytes(file, uint(out.DataLen)))

	return out
}

type XAuthority struct {
	Family  uint16
	AddrLen uint16
	Address string
	DispLen uint16
	Display string
	NameLen uint16
	Name    string
	DataLen uint16
	Data    string
}

func main() {
	var conn, err = OpenSocket(GetSocketLocation())
	if err != nil {
		panic(err)
	}
	var connection = StartConn(conn, ParseXauthorityFile(GetXauthorityFile()))
}
