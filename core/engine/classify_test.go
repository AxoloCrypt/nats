package engine

import "testing"

// TestClassify_Taxonomy covers at least one example per v1 taxonomy entry
// (FR-7: router, phone, printer, IoT device, smart TV, computer) plus the
// insufficient-signal -> "unknown" case, exercising Classify as the pure
// function it is (AD-9): a plain Device in, a DeviceType string out.
func TestClassify_Taxonomy(t *testing.T) {
	tests := []struct {
		name string
		d    Device
		want string
	}{
		{
			name: "router from SSDP InternetGatewayDevice service type",
			d: Device{
				MAC:         "aa:bb:cc:dd:ee:ff",
				ServiceData: map[string]string{"type": "urn:schemas-upnp-org:device:InternetGatewayDevice:1"},
			},
			want: DeviceTypeRouter,
		},
		{
			name: "phone from Apple vendor",
			d: Device{
				MAC:    "aa:bb:cc:dd:ee:ff",
				Vendor: "Apple, Inc.",
			},
			want: DeviceTypePhone,
		},
		{
			name: "printer from raw/JetDirect open port 9100",
			d: Device{
				MAC:       "aa:bb:cc:dd:ee:ff",
				OpenPorts: []OpenPort{{Port: 9100, Protocol: "tcp", State: "open"}},
			},
			want: DeviceTypePrinter,
		},
		{
			name: "IoT device from mDNS HomeKit accessory service type",
			d: Device{
				MAC:         "aa:bb:cc:dd:ee:ff",
				ServiceData: map[string]string{"name": "bulb1._hap._tcp.local."},
			},
			want: DeviceTypeIoT,
		},
		{
			name: "smart TV from Chromecast control port 8009",
			d: Device{
				MAC:       "aa:bb:cc:dd:ee:ff",
				OpenPorts: []OpenPort{{Port: 8009, Protocol: "tcp", State: "open"}},
			},
			want: DeviceTypeSmartTV,
		},
		{
			name: "computer from Dell vendor",
			d: Device{
				MAC:    "aa:bb:cc:dd:ee:ff",
				Vendor: "Dell Inc.",
			},
			want: DeviceTypeComputer,
		},
		{
			name: "unknown when signal is insufficient",
			d: Device{
				IP:  "10.0.0.5",
				MAC: "aa:bb:cc:dd:ee:ff",
			},
			want: DeviceTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.d)
			if got != tt.want {
				t.Fatalf("Classify(%+v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestClassify_OpenPortOutranksVendorSignal(t *testing.T) {
	// A Dell-manufactured print server would otherwise match the
	// "computer" vendor keyword; the concrete printing-port signal must
	// win, confirming ports are checked ahead of vendor keywords.
	d := Device{
		Vendor:    "Dell Inc.",
		OpenPorts: []OpenPort{{Port: 631, Protocol: "tcp", State: "open"}},
	}

	if got := Classify(d); got != DeviceTypePrinter {
		t.Fatalf("Classify(%+v) = %q, want %q (open port should outrank vendor)", d, got, DeviceTypePrinter)
	}
}

func TestClassify_ClosedPortDoesNotMatchPortSignature(t *testing.T) {
	// A port merely recorded as "closed" (e.g. by tcpconnect) must not be
	// treated the same as a confirmed-open one for port-based signatures.
	d := Device{
		OpenPorts: []OpenPort{{Port: 9100, Protocol: "tcp", State: "closed"}},
	}

	if got := Classify(d); got != DeviceTypeUnknown {
		t.Fatalf("Classify(%+v) = %q, want %q for a closed port", d, got, DeviceTypeUnknown)
	}
}

func TestClassify_UDPPortDoesNotMatchTCPPortSignature(t *testing.T) {
	// The printer/smart-tv port signatures are inherently TCP-based
	// services (IPP, JetDirect, RTSP-TCP, Chromecast control); a same-numbered
	// open UDP entry must not satisfy them.
	d := Device{
		OpenPorts: []OpenPort{{Port: 9100, Protocol: "udp", State: "open"}},
	}

	if got := Classify(d); got != DeviceTypeUnknown {
		t.Fatalf("Classify(%+v) = %q, want %q for a UDP port matching a TCP-only signature", d, got, DeviceTypeUnknown)
	}
}

func TestClassify_ServiceDataOutranksVendorSignal(t *testing.T) {
	// A router with an authoritative SSDP InternetGatewayDevice claim would
	// otherwise match the "computer" vendor keyword if Vendor were checked
	// first; the ServiceData claim must win, confirming ServiceData is
	// checked ahead of Vendor.
	d := Device{
		Vendor:      "Dell Inc.",
		ServiceData: map[string]string{"type": "urn:schemas-upnp-org:device:InternetGatewayDevice:1"},
	}

	if got := Classify(d); got != DeviceTypeRouter {
		t.Fatalf("Classify(%+v) = %q, want %q (ServiceData should outrank vendor)", d, got, DeviceTypeRouter)
	}
}

func TestClassify_BannerSignal(t *testing.T) {
	// enrich/banner (Story 2.3) grabs a raw first-read from an already-open
	// TCP port; a dropbear SSH banner is a common signature of embedded
	// router/IoT firmware distinct from a desktop OpenSSH banner.
	d := Device{
		OpenPorts: []OpenPort{{Port: 22, Protocol: "tcp", State: "open", Banner: "SSH-2.0-dropbear_2020.81"}},
	}

	if got := Classify(d); got != DeviceTypeRouter {
		t.Fatalf("Classify(%+v) = %q, want %q from a dropbear SSH banner", d, got, DeviceTypeRouter)
	}
}

func TestClassify_BannerOutranksVendorSignal(t *testing.T) {
	d := Device{
		Vendor:    "Dell Inc.",
		OpenPorts: []OpenPort{{Port: 22, Protocol: "tcp", State: "open", Banner: "SSH-2.0-dropbear_2020.81"}},
	}

	if got := Classify(d); got != DeviceTypeRouter {
		t.Fatalf("Classify(%+v) = %q, want %q (banner should outrank vendor)", d, got, DeviceTypeRouter)
	}
}

func TestClassify_IsPureNoMutationOfInput(t *testing.T) {
	d := Device{IP: "10.0.0.5", MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "bogus-preset-value"}

	_ = Classify(d)

	if d.DeviceType != "bogus-preset-value" {
		t.Fatalf("Classify must not mutate its input Device, DeviceType changed to %q", d.DeviceType)
	}
}
