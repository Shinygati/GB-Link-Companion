package io.gblink.companion;
import android.hardware.usb.*;
import java.io.*;
import java.util.Arrays;

/** Android USB host CDC-ACM, using firmware VID 2FE3 from upstream link.go. */
final class UsbPort implements Wire.Port {
    final UsbDeviceConnection connection;
    UsbInterface control,data;
    UsbEndpoint in,out;
    volatile boolean closed;
    UsbPort(UsbManager manager,UsbDevice device)throws IOException{
        connection=manager.openDevice(device);
        if(connection==null)throw new IOException("USB permission is missing or the adapter was unplugged");
        try {
            for(int i=0;i<device.getInterfaceCount();i++){
                UsbInterface face=device.getInterface(i);
                if(face.getInterfaceClass()==UsbConstants.USB_CLASS_COMM)control=face;
                if(face.getInterfaceClass()!=UsbConstants.USB_CLASS_CDC_DATA)continue;
                UsbEndpoint rx=null,tx=null;
                for(int j=0;j<face.getEndpointCount();j++){
                    UsbEndpoint ep=face.getEndpoint(j);
                    if(ep.getType()!=UsbConstants.USB_ENDPOINT_XFER_BULK)continue;
                    if(ep.getDirection()==UsbConstants.USB_DIR_IN)rx=ep;else tx=ep;
                }
                if(rx!=null&&tx!=null){data=face;in=rx;out=tx;}
            }
            if(control==null||data==null)throw new IOException("Adapter has no CDC-ACM serial interface");
            if(!connection.claimInterface(control,true)||!connection.claimInterface(data,true))throw new IOException("Cannot claim GB-Link USB interface");
            byte[] line={0,(byte)0xC2,1,0,0,0,8}; // 115200, 8N1
            if(connection.controlTransfer(0x21,0x20,0,control.getId(),line,7,1000)!=7)throw new IOException("CDC line coding failed");
            if(connection.controlTransfer(0x21,0x22,3,control.getId(),null,0,1000)<0)throw new IOException("CDC control-line setup failed");
        }catch(IOException e){close();throw e;}
    }
    public void write(byte[] b)throws IOException{
        if(closed)throw new IOException("GB-Link disconnected");
        int offset=0;while(offset<b.length){int n=connection.bulkTransfer(out,b,offset,b.length-offset,2000);if(n<=0)throw new IOException("USB write failed; save remains pending");offset+=n;}
    }
    public byte[] read(int timeout)throws IOException{
        if(closed)throw new IOException("GB-Link disconnected");
        byte[] b=new byte[4096];int n=connection.bulkTransfer(in,b,b.length,timeout);
        if(closed)throw new IOException("GB-Link disconnected");return n>0?Arrays.copyOf(b,n):new byte[0];
    }
    public void close(){closed=true;if(data!=null)connection.releaseInterface(data);if(control!=null)connection.releaseInterface(control);connection.close();}
}
