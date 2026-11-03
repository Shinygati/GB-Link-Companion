package io.gblink.companion;
import android.hardware.usb.*;
import java.io.*;
import java.nio.*;

/** RP2040 BOOTSEL USB protocol, based on Raspberry Pi picotool (BSD-3-Clause).
 * Copyright (c) 2020 Raspberry Pi (Trading) Ltd. See assets/NOTICE-picotool.txt. */
final class Picoboot implements FirmwareImage.Boot,AutoCloseable {
    final UsbDeviceConnection connection;UsbInterface face;UsbEndpoint in,out;int token=1;
    boolean xip;
    static boolean matches(UsbDevice d){return d.getVendorId()==0x2E8A&&d.getProductId()==0x0003;}
    Picoboot(UsbManager manager,UsbDevice d)throws IOException{
        if(!matches(d))throw new IOException("Only the RP2040 BOOTSEL bootloader is supported");
        connection=manager.openDevice(d);if(connection==null)throw new IOException("USB bootloader permission missing");
        try{
            for(int i=0;i<d.getInterfaceCount();i++){
                UsbInterface f=d.getInterface(i);if(f.getInterfaceClass()!=255||f.getEndpointCount()!=2)continue;
                UsbEndpoint rx=null,tx=null;
                for(int j=0;j<f.getEndpointCount();j++){UsbEndpoint e=f.getEndpoint(j);if(e.getType()!=UsbConstants.USB_ENDPOINT_XFER_BULK)continue;if(e.getDirection()==UsbConstants.USB_DIR_IN)rx=e;else tx=e;}
                if(rx!=null&&tx!=null){face=f;in=rx;out=tx;break;}
            }
            if(face==null||!connection.claimInterface(face,true))throw new IOException("Cannot claim RP2040 PICOBOOT interface");
            // Clear endpoint halts and reset the boot protocol.
            connection.controlTransfer(0x02,1,0,in.getAddress(),null,0,1000);
            connection.controlTransfer(0x02,1,0,out.getAddress(),null,0,1000);
            if(connection.controlTransfer(0x41,0x41,0,face.getId(),null,0,1000)!=0)throw new IOException("Cannot reset PICOBOOT interface");
            cmd(1,new byte[]{1},null,0);
        }catch(IOException e){close();throw e;}
    }
    static byte[] ints(int...values){ByteBuffer b=ByteBuffer.allocate(values.length*4).order(ByteOrder.LITTLE_ENDIAN);for(int v:values)b.putInt(v);return b.array();}
    byte[] cmd(int id,byte[] args,byte[] data,int readLength)throws IOException{
        int transfer=(id&128)!=0?readLength:(data==null?0:data.length),sentToken=token++;
        ByteBuffer c=ByteBuffer.allocate(32).order(ByteOrder.LITTLE_ENDIAN);c.putInt(0x431FD10B).putInt(sentToken).put((byte)id).put((byte)args.length).putShort((short)0).putInt(transfer).put(args);
        if(connection.bulkTransfer(out,c.array(),32,3000)!=32)throw failure("command",id);
        byte[] result=new byte[readLength];
        if(transfer!=0){
            byte[] buffer=(id&128)!=0?result:data;UsbEndpoint endpoint=(id&128)!=0?in:out;int offset=0;
            while(offset<transfer){int n=connection.bulkTransfer(endpoint,buffer,offset,transfer-offset,10000);if(n<=0)throw failure("data",id);offset+=n;}
        }
        byte[] ack=new byte[1];UsbEndpoint endpoint=(id&128)!=0?out:in;
        // A read's ACK is a one-byte OUT transfer; a write's ACK is a zero-length IN packet.
        int n=connection.bulkTransfer(endpoint,ack,1,10000);
        if(n<0||((id&128)==0&&n!=0))throw failure("acknowledgement",id);
        if(id!=2){
            byte[] status=new byte[16];int got=connection.controlTransfer(0xC1,0x42,0,face.getId(),status,16,1000);
            ByteBuffer s=ByteBuffer.wrap(status).order(ByteOrder.LITTLE_ENDIAN);
            if(got!=16||s.getInt(0)!=sentToken||s.getInt(4)!=0||status[9]!=0)throw new IOException("RP2040 rejected firmware command "+id);
        }
        return result;
    }
    IOException failure(String stage,int cmd){return new IOException("RP2040 USB "+stage+" failed ("+cmd+"). Re-enter BOOTSEL and reinstall; phone saves are retained.");}
    void exitXip()throws IOException{cmd(6,new byte[0],null,0);xip=false;}
    public byte[] read(int addr,int n)throws IOException{if(!xip){cmd(7,new byte[0],null,0);xip=true;}return cmd(0x84,ints(addr,n),null,n);}
    public void erase(int addr,int n)throws IOException{exitXip();cmd(3,ints(addr,n),null,0);}
    public void write(int addr,byte[] data)throws IOException{if(xip)exitXip();cmd(5,ints(addr,data.length),data,0);}
    public void reboot()throws IOException{cmd(2,ints(0,0,500),null,0);}
    public void close(){if(face!=null)connection.releaseInterface(face);connection.close();}
}
