package io.gblink.companion;

import java.io.*;
import java.nio.*;
import java.util.*;

/** GB-Link firmware framing and the upstream GBA helper protocol. */
class Wire {
    interface Port { void write(byte[] b) throws IOException; byte[] read(int timeout) throws IOException; void close(); }
    interface Progress { void update(int done,int total) throws IOException; }
    static final int POLL=0x504F4C4C, IDLE=0x49444C45;
    static final int[] SIZES={0,32768,65536,65536,131072,512,8192};
    static final String[] TYPES={"No save chip", "SRAM / FRAM 32 KB", "SRAM 64 KB", "Flash 64 KB", "Flash 128 KB", "EEPROM 512 B", "EEPROM 8 KB"};
    static final String[] ERRORS={"OK","Unknown flash chip","Timed out","Checksum error","Write failed","Verify failed","Bad command","Out of range","No save chip"};
    final Port port;
    final Decoder decoder=new Decoder();
    int gap=120;
    Wire(Port port){this.port=port;}
    static int checksum(int c,int w){return Integer.rotateLeft(c,5)+w;}
    static byte[] frame(int channel,byte[] payload){
        byte[] b=new byte[payload.length+5];b[0]='G';b[1]='B';b[2]=(byte)channel;b[3]=(byte)payload.length;b[4]=(byte)(payload.length>>>8);
        System.arraycopy(payload,0,b,5,payload.length);return b;
    }
    static class Decoder {
        byte[] pending=new byte[0]; final ArrayDeque<byte[]> data=new ArrayDeque<>();
        void feed(byte[] b){
            byte[] all=Arrays.copyOf(pending,pending.length+b.length);System.arraycopy(b,0,all,pending.length,b.length);
            int i=0;
            while(i+5<=all.length){
                if(all[i]!='G'||all[i+1]!='B'){i++;continue;}
                int n=(all[i+3]&255)|((all[i+4]&255)<<8);
                if(n>64){i++;continue;}
                if(i+5+n>all.length)break;
                if(all[i+2]==1)data.add(Arrays.copyOfRange(all,i+5,i+5+n));
                i+=5+n;
            }
            pending=Arrays.copyOfRange(all,i,all.length);
            if(pending.length>68)pending=new byte[0];
        }
    }
    void commandFrame(int...values)throws IOException{byte[] b=new byte[values.length];for(int i=0;i<b.length;i++)b[i]=(byte)values[i];port.write(frame(0,b));}
    String firmwareVersion()throws IOException{
        drain();commandFrame(0x0F);long end=System.nanoTime()+1_500_000_000L;
        while(System.nanoTime()<end){
            while(!decoder.data.isEmpty()){byte[] b=decoder.data.remove();if(b.length>=4&&b[0]==0x0F)return (b[1]&255)+"."+(b[2]&255)+"."+(b[3]&255);}
            decoder.feed(port.read(100));
        }
        return "unknown";
    }
    void timing(int us)throws IOException{commandFrame(0x30,us,us>>>8,us>>>16,4);}
    void configure()throws IOException{commandFrame(0x00,2);sleep(100);timing(gap);sleep(20);drain();}
    void drain()throws IOException{decoder.pending=new byte[0];decoder.data.clear();for(int i=0;i<32;i++){if(port.read(20).length==0)break;}}
    int[] words(int...tx)throws IOException{
        int[] out=new int[tx.length];
        for(int off=0;off<tx.length;off+=15){
            int n=Math.min(15,tx.length-off);ByteBuffer b=ByteBuffer.allocate(n*4).order(ByteOrder.BIG_ENDIAN);
            for(int i=0;i<n;i++)b.putInt(tx[off+i]);
            port.write(frame(1,b.array()));
            ByteArrayOutputStream reply=new ByteArrayOutputStream();long end=System.nanoTime()+2_000_000_000L;
            while(reply.size()<n*4){
                while(!decoder.data.isEmpty())reply.write(decoder.data.remove());
                if(reply.size()>=n*4)break;
                if(System.nanoTime()>end)throw new IOException("GB-Link disconnected or stopped answering. Your phone save is retained.");
                decoder.feed(port.read(200));
            }
            if(reply.size()!=n*4)throw new IOException("Unexpected GB-Link reply length; reconnect the adapter.");
            ByteBuffer r=ByteBuffer.wrap(reply.toByteArray()).order(ByteOrder.BIG_ENDIAN);
            for(int i=0;i<n;i++)out[off+i]=r.getInt();
        }
        return out;
    }
    int word(int w)throws IOException{return words(w)[0];}
    static void sleep(long ms)throws IOException{try{Thread.sleep(ms);}catch(InterruptedException e){Thread.currentThread().interrupt();throw new IOException("Cancelled",e);}}
    boolean idle(long ms)throws IOException{
        long end=System.nanoTime()+ms*1_000_000;
        do{int[] r=words(POLL,POLL,POLL,POLL);if(r[3]==IDLE)return true;sleep(5);}while(System.nanoTime()<end);
        return false;
    }
    int[] packet(int cmd,int[] args,int[] data)throws IOException{
        int[] p=new int[args.length+data.length+2];p[0]=0x42000000|(cmd<<16)|args.length;
        System.arraycopy(args,0,p,1,args.length);System.arraycopy(data,0,p,1+args.length,data.length);
        int ck=0x12345678;for(int i=0;i<p.length-1;i++)ck=checksum(ck,p[i]);p[p.length-1]=ck;
        return p;
    }
    static final class Slip extends IOException { Slip(){super("Link data out of step");} }
    int[] call(int cmd,int[] args,int[] data,int timeoutMs,int...counts)throws IOException{
        for(int attempt=0;attempt<4;attempt++){
            if(!idle(8000))throw new IOException("GBA helper is not responding. Restart the GBA holding START + SELECT.");
            int[] p=packet(cmd,args,data), r=words(p);boolean ack=true;
            for(int i=0;i<r.length;i++)if(r[i]!=(i==0?IDLE:0xA5000000|i))ack=false;
            if(!ack){gap=Math.min(300,gap+40);timing(gap);sleep(400);continue;}
            try{return result(timeoutMs,counts);}catch(Slip e){sleep(400);}
        }
        throw new IOException("The link kept failing. Check the cable and cartridge contacts.");
    }
    int[] result(int timeoutMs,int...counts)throws IOException{
        long end=System.nanoTime()+timeoutMs*1_000_000L;
        ArrayList<Integer> pending=new ArrayList<>();int need=15;
        while(System.nanoTime()<end){
            int[] polls=new int[need];Arrays.fill(polls,POLL);for(int v:words(polls))pending.add(v);
            need=15;boolean waiting=false;
            for(int i=0;i<pending.size();i++){
                int w=pending.get(i);if((w>>>24)!=0x52)continue;
                int st=(w>>>16)&255,n=w&65535;boolean allowed=false;for(int c:counts)if(n==c)allowed=true;
                if(st>=ERRORS.length || (st==0&&!allowed) || n>8193)continue;
                if(i+n+1>=pending.size()){
                    pending=new ArrayList<>(pending.subList(i,pending.size()));need=Math.max(15,Math.min(240,n+2-pending.size()));waiting=true;break;
                }
                if(pending.get(i+n+1)!=IDLE)throw new Slip();
                if(st==3)throw new Slip();
                if(st!=0)throw new IOException("Cartridge: "+ERRORS[st]+" (command "+cmdName(n)+")");
                int[] d=new int[n];for(int j=0;j<n;j++)d[j]=pending.get(i+1+j);return d;
            }
            if(!waiting&&pending.size()>64)pending=new ArrayList<>(pending.subList(pending.size()-32,pending.size()));
        }
        throw new IOException("GBA stopped responding during cartridge operation.");
    }
    private String cmdName(int n){return "reply "+n;}
    boolean alive()throws IOException{
        configure();if(!idle(500))return false;
        call(1,new int[0],new int[0],3000,1);return true;
    }
    void start(byte[] image,Progress progress)throws IOException{
        commandFrame(0x40);
        if(!alive()){
            multiboot(image,progress);configure();
            if(!idle(8000))throw new IOException("The GBA helper did not start. Check the link cable and retry.");
            call(1,new int[0],new int[0],3000,1);
        }
        // Controller screen, without advertising the omitted status menu.
        call(9,new int[]{1,0},new int[0],3000,0);
    }
    void multiboot(byte[] data,Progress progress)throws IOException{
        if(data.length<0x190||data.length>0x40000)throw new IOException("Invalid GBA helper image");
        commandFrame(0,2);sleep(100);timing(36);sleep(50);drain();
        long end=System.nanoTime()+30_000_000_000L;boolean found=false;
        while(System.nanoTime()<end){progress.update(0,data.length);if((word(0x6202)>>>16)==0x7202){found=true;break;}sleep(10);}
        if(!found)throw new IOException("Switch the GBA on while holding START + SELECT, then retry.");
        word(0x6102);byte[] padded=Arrays.copyOf(data,(data.length+15)&~15);
        int[] hdr=new int[0x60];for(int i=0;i<hdr.length;i++)hdr[i]=(padded[2*i]&255)|((padded[2*i+1]&255)<<8);words(hdr);
        word(0x6200);word(0x6202);word(0x63D1);int token=word(0x63D1);
        if((token>>>24)!=0x73)throw new IOException("Multiboot handshake failed");
        int a=(token>>>16)&255,seed=0xFFFF00D1|(a<<8);a=(a+15)&255;
        word(0x6400|a);int b=(word((padded.length-0x190)/4)>>>16)&255,crc=0xC387;
        ByteBuffer raw=ByteBuffer.wrap(padded).order(ByteOrder.LITTLE_ENDIAN);
        for(int i=0xC0;i<padded.length;){
            int n=Math.min(15,(padded.length-i)/4);int[] batch=new int[n];
            for(int j=0;j<n;j++){int off=i+j*4,v=raw.getInt(off);crc=crcStep(crc,v);seed=seed*0x6F646573+1;batch[j]=seed^v^(0xFE000000-off)^0x43202F2F;}
            int[] res=words(batch);for(int j=0;j<n;j++)if((res[j]>>>16)!=((i+j*4)&65535))throw new IOException("Multiboot transfer failed");
            i+=n*4;progress.update(i,data.length);
        }
        crc=crcStep(crc,0xFFFF0000|(b<<8)|a);word(0x65);end=System.nanoTime()+10_000_000_000L;
        while((word(0x65)>>>16)!=0x75){if(System.nanoTime()>end)throw new IOException("GBA rejected helper");sleep(10);}
        word(0x66);word(crc&65535);sleep(1000);
    }
    static int crcStep(int crc,int v){for(int i=0;i<32;i++){int bit=(crc^v)&1;crc>>>=1;if(bit!=0)crc^=0xC37B;v>>>=1;}return crc;}
    static byte[] chunk(int[] data,int n)throws IOException{
        if(n%4!=0||data.length<2||(data.length!=n/4+1&&data.length!=2))throw new IOException("Invalid cartridge chunk");
        ByteBuffer b=ByteBuffer.allocate(n).order(ByteOrder.LITTLE_ENDIAN);int ck=0x12345678;
        for(int i=0;i<n/4;i++){int w=data.length==2?data[1]:data[i+1];b.putInt(w);ck=checksum(ck,w);}
        if(ck!=data[0])throw new IOException("Cartridge transfer checksum mismatch");return b.array();
    }
    static final class Cart {
        int type,size;byte[] header;boolean guessed;
        String title(){return text(header,0xA0,12);}String code(){return text(header,0xAC,4);}
        String identity(){return Store.hash(header);}
    }
    Cart cart()throws IOException{
        int[] info=call(6,new int[0],new int[0],60000,6),rom=call(10,new int[0],new int[0],30000,49);
        if(rom[0]==-1)throw new IOException("No cartridge found");
        Cart c=new Cart();c.type=info[0];if(c.type<0||c.type>6)throw new IOException("Unsupported save chip");
        c.size=rom[0]&0x3FFFFFFF;c.guessed=(rom[0]&0x80000000)!=0;
        if(c.size<192||c.size>32*1024*1024)throw new IOException("Invalid cartridge ROM size");
        ByteBuffer h=ByteBuffer.allocate(192).order(ByteOrder.LITTLE_ENDIAN);for(int i=1;i<rom.length;i++)h.putInt(rom[i]);c.header=h.array();
        ByteBuffer s=ByteBuffer.allocate(16).order(ByteOrder.LITTLE_ENDIAN);for(int i=2;i<6;i++)s.putInt(info[i]);
        if(!Arrays.equals(s.array(),Arrays.copyOfRange(c.header,0xA0,0xB0)))throw new IOException("Cartridge changed during detection");
        return c;
    }
    static String text(byte[] a,int off,int n){return new String(a,off,n,java.nio.charset.StandardCharsets.US_ASCII).replaceAll("[^ -~]"," ").trim();}
    byte[] readSave(int type,Progress progress)throws IOException{
        int size=SIZES[type];byte[] out=new byte[size];int steps=(size+16383)/16384;
        for(int off=0,k=0;off<size;off+=16384,k++){
            int n=Math.min(16384,size-off);byte[] b=null;
            for(int tries=0;tries<3;tries++){
                int[] d=call(7,new int[]{type,off,n,k,steps},new int[0],20000,0,2,n/4+1);
                try{b=chunk(d,n);break;}catch(IOException e){if(tries==2)throw e;}
            }
            System.arraycopy(b,0,out,off,n);progress.update(off+n,size);
        }
        return out;
    }
    void writeBlock(int type,int offset,byte[] b,int step,int steps)throws IOException{
        ByteBuffer v=ByteBuffer.wrap(b).order(ByteOrder.LITTLE_ENDIAN);int[] d=new int[b.length/4];for(int i=0;i<d.length;i++)d[i]=v.getInt();
        call(8,new int[]{type,offset,b.length,step,steps},d,60000,0,3);
    }
    void dump(Cart c,OutputStream out,Progress progress)throws IOException{
        int steps=(c.size+32767)/32768;
        for(int off=0,k=0;off<c.size;off+=32768,k++){
            int n=Math.min(32768,c.size-off);out.write(chunk(call(11,new int[]{off,n,k,steps},new int[0],20000,2,n/4+1),n));progress.update(off+n,c.size);
        }
    }
    int keys()throws IOException{int[] r=words(0x4B455953,POLL);return r[0]==IDLE&&(r[1]>>>24)==0x4B?r[1]&1023:0;}
}
