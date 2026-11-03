package io.gblink.companion;
import java.io.*;
import java.nio.*;
import java.util.*;

/** Strict RP2040 UF2 parser; accepts only the bundled, hashed official image. */
final class FirmwareImage {
    static final String VERSION="2.2.3",FILE="gblink.v2.2.3.uf2";
    static final String SHA256="6600b227795434d202b186835c45bf4e3dfa505711c773a195592ccdf5df2113";
    final TreeMap<Integer,byte[]> pages=new TreeMap<>();
    FirmwareImage(byte[] bytes,String expectedHash)throws IOException{
        if(!Store.hash(bytes).equals(expectedHash))throw new IOException("Firmware SHA-256 verification failed");
        if(bytes.length==0||bytes.length%512!=0)throw new IOException("Invalid UF2 length");
        int total=bytes.length/512;BitSet seen=new BitSet(total);ByteBuffer b=ByteBuffer.wrap(bytes).order(ByteOrder.LITTLE_ENDIAN);
        for(int i=0;i<total;i++){
            int off=i*512,flags=b.getInt(off+8),address=b.getInt(off+12),size=b.getInt(off+16),block=b.getInt(off+20),count=b.getInt(off+24);
            if(b.getInt(off)!=0x0A324655||b.getInt(off+4)!=0x9E5D5157||b.getInt(off+508)!=0x0AB16F30||flags!=0x2000||b.getInt(off+28)!=0xE48BFF56)throw new IOException("Firmware is not an RP2040 main-flash UF2");
            if(size!=256||(address&255)!=0||address<0x10000000||address>0x101FFF00||count!=total||block<0||block>=total||seen.get(block)||pages.containsKey(address))throw new IOException("Invalid or duplicate UF2 block");
            seen.set(block);pages.put(address,Arrays.copyOfRange(bytes,off+32,off+32+256));
        }
        if(seen.cardinality()!=total||pages.firstKey()!=0x10000000)throw new IOException("Incomplete firmware image");
    }
    TreeSet<Integer> sectors(){TreeSet<Integer> s=new TreeSet<>();for(int addr:pages.keySet())s.add(addr&~4095);return s;}
    byte[] merge(int sector,byte[] old)throws IOException{
        if(old.length!=4096)throw new IOException("Invalid firmware sector size");byte[] out=old.clone();
        for(Map.Entry<Integer,byte[]> p:pages.subMap(sector,true,sector+4096,false).entrySet())System.arraycopy(p.getValue(),0,out,p.getKey()-sector,256);return out;
    }
    interface Boot {
        byte[] read(int addr,int n)throws IOException;
        void erase(int addr,int n)throws IOException;
        void write(int addr,byte[] data)throws IOException;
        void reboot()throws IOException;
    }
    void install(Boot boot,File backup,Wire.Progress progress)throws IOException{
        // Read every affected sector before any erase. Preserve uncovered UF2 gaps.
        TreeMap<Integer,byte[]> original=new TreeMap<>();ByteArrayOutputStream bytes=new ByteArrayOutputStream();DataOutputStream archive=new DataOutputStream(bytes);
        archive.writeUTF("GB-Link RP2040 pre-update sectors v1");archive.writeInt(sectors().size());
        for(int addr:sectors()){
            byte[] old=boot.read(addr,4096);if(old.length!=4096)throw new IOException("Incomplete firmware backup read");original.put(addr,old);archive.writeInt(addr);archive.write(old);
        }
        archive.flush();Store.atomic(backup,bytes.toByteArray());
        int done=0;
        for(Map.Entry<Integer,byte[]> e:original.entrySet()){
            byte[] target=merge(e.getKey(),e.getValue());
            if(!Arrays.equals(target,e.getValue())){
                boot.erase(e.getKey(),4096);boot.write(e.getKey(),target);
                if(!Arrays.equals(target,boot.read(e.getKey(),4096)))throw new IOException("Firmware verification failed. Re-enter BOOTSEL and reinstall.");
            }
            progress.update(++done,original.size());
        }
        boot.reboot();
    }
}
