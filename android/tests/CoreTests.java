package io.gblink.companion;

import java.io.*;
import java.nio.*;
import java.nio.file.*;
import java.util.*;

/** Host tests for save integrity, disconnect recovery and the actual wire codec. */
public final class CoreTests {
    static int checks;
    static void check(boolean value,String message){checks++;if(!value)throw new AssertionError(message);}
    interface Throwing {void run()throws Exception;}
    static <T extends Throwable> T expect(Class<T> type,Throwing run)throws Exception{
        try{run.run();}catch(Throwable e){check(type.isInstance(e),"Expected "+type+", got "+e);return type.cast(e);}throw new AssertionError("Expected "+type);
    }
    static final Wire.Progress NO_PROGRESS=(d,t)->{};
    static byte[] rom(){byte[] b=new byte[1024];b[0xB2]=(byte)0x96;System.arraycopy("TESTGAME    TEST".getBytes(java.nio.charset.StandardCharsets.US_ASCII),0,b,0xA0,16);int c=0;for(int i=0xA0;i<=0xBC;i++)c-=b[i]&255;b[0xBD]=(byte)(c-0x19);return b;}
    static class FakeWire extends Wire {
        Cart cart;byte[] ram;int writes,failAt=-1;boolean failAfter;Runnable duringWrite;
        FakeWire(int type){super(null);cart=new Cart();cart.type=type;cart.size=1024;cart.header=Arrays.copyOf(rom(),192);ram=new byte[SIZES[type]];Arrays.fill(ram,(byte)0xA5);}
        @Override Cart cart(){return cart;}
        @Override byte[] readSave(int t,Progress p){return ram.clone();}
        @Override void writeBlock(int type,int offset,byte[] b,int k,int n)throws IOException{
            if(writes==failAt&&!failAfter){writes++;throw new IOException("USB unplugged before block");}
            System.arraycopy(b,0,ram,offset,b.length);
            if(duringWrite!=null){Runnable r=duringWrite;duringWrite=null;r.run();}
            if(writes++==failAt&&failAfter)throw new IOException("USB unplugged after block");
        }
    }
    static Store store()throws IOException{return new Store(Files.createTempDirectory("gblink-tests-").toFile());}
    static String begin(Store s,FakeWire w)throws IOException{File f=s.importRom(rom());String id=Store.id(f);s.begin(id,w.cart,w.ram);return id;}
    static void codec()throws Exception{
        byte[] payload={1,2,3,4,5,6,7,8};byte[] f=Wire.frame(1,payload);
        for(int split=0;split<=f.length;split++){
            Wire.Decoder d=new Wire.Decoder();d.feed(Arrays.copyOf(f,split));d.feed(Arrays.copyOfRange(f,split,f.length));check(Arrays.equals(d.data.remove(),payload),"Fragment split "+split);
        }
        Wire.Decoder d=new Wire.Decoder();d.feed(new byte[]{99,71,66,1,(byte)255,127});d.feed(Wire.frame(0,new byte[]{1}));d.feed(f);check(d.data.size()==1,"Resync and ignore command channel");
        int[] words={0,0xDEADBEEF,0x12345678};int ck=0x12345678;for(int i=1;i<words.length;i++)ck=Wire.checksum(ck,words[i]);words[0]=ck;
        byte[] b=Wire.chunk(words,8);check(ByteBuffer.wrap(b).order(ByteOrder.LITTLE_ENDIAN).getInt()==0xDEADBEEF,"ROM little-endian");
        int fill=0xFFFFFFFF;ck=0x12345678;for(int i=0;i<64;i++)ck=Wire.checksum(ck,fill);byte[] blank=Wire.chunk(new int[]{ck,fill},256);check(blank[255]==(byte)255,"Compressed repeated word");
        expect(IOException.class,()->Wire.chunk(new int[]{1,2},256));
        Store.validateRom(rom());byte[] bad=rom();bad[0xA0]^=1;expect(IOException.class,()->Store.validateRom(bad));
    }
    static void sizes()throws Exception{
        for(int type=1;type<=6;type++){
            Store s=store();FakeWire w=new FakeWire(type);String id=begin(s,w);byte[] next=w.ram.clone();next[0]=4;next[next.length-1]=7;
            s.emulatorSaved(id,next);CartridgeSync.write(s,id,w,null,NO_PROGRESS);
            check(Arrays.equals(w.ram,next),"Chip type "+type+" complete write");check(!s.entry(id).pending(),"Verified save committed");check(s.backups.list().length>=5,"Read, emulator and pre-write backups");
            int count=s.backups.list().length;s.emulatorSaved(id,next);check(s.backups.list().length==count+1,"Identical in-game save backed up too");
        }
    }
    static void disconnect(boolean after)throws Exception{
        Store s=store();FakeWire w=new FakeWire(4);String id=begin(s,w);byte[] next=w.ram.clone();Arrays.fill(next,(byte)0x19);s.checkpoint(id,next);
        w.failAt=1;w.failAfter=after;expect(IOException.class,()->CartridgeSync.write(s,id,w,null,NO_PROGRESS));
        Store reopened=new Store(s.root);check(reopened.entry(id).pending(),"Pending survives restart");check(Arrays.equals(reopened.data(reopened.entry(id)),next),"Latest phone save survives USB loss");
        w.failAt=-1;CartridgeSync.write(reopened,id,w,null,NO_PROGRESS);check(!reopened.entry(id).pending(),"Retry completes after disconnect");check(Arrays.equals(w.ram,next),"Retry writes exact bytes");
    }
    static void conflicts()throws Exception{
        Store s=store();FakeWire w=new FakeWire(1);String id=begin(s,w);byte[] next=w.ram.clone();next[0]=42;s.checkpoint(id,next);w.ram[3]=1;
        CartridgeSync.Conflict c=expect(CartridgeSync.Conflict.class,()->CartridgeSync.write(s,id,w,null,NO_PROGRESS));check(w.writes==0,"Conflict never writes");
        w.ram[5]=99;expect(CartridgeSync.Conflict.class,()->CartridgeSync.write(s,id,w,c.currentHash,NO_PROGRESS));check(w.writes==0,"Stale force confirmation rejected");
        String force=Store.hash(w.ram);CartridgeSync.write(s,id,w,force,NO_PROGRESS);check(Arrays.equals(w.ram,next),"Explicit conflict replacement");
        next[1]=44;s.checkpoint(id,next);w.cart.header=w.cart.header.clone();w.cart.header[0xBC]++;
        expect(IOException.class,()->CartridgeSync.write(s,id,w,Store.hash(w.ram),NO_PROGRESS));check(s.entry(id).pending(),"Wrong cartridge preserves pending");
    }
    static void newerDuringWrite()throws Exception{
        Store s=store();FakeWire w=new FakeWire(1);String id=begin(s,w);byte[] first=w.ram.clone();first[0]=13;byte[] second=first.clone();second[1]=14;s.checkpoint(id,first);
        w.duringWrite=()->{try{s.checkpoint(id,second);}catch(IOException e){throw new RuntimeException(e);}};
        CartridgeSync.write(s,id,w,null,NO_PROGRESS);check(s.entry(id).pending(),"Newer mGBA save remains pending during write");check(Arrays.equals(s.data(s.entry(id)),second),"Newer snapshot retained");
        CartridgeSync.write(s,id,w,null,NO_PROGRESS);check(Arrays.equals(w.ram,second),"Newer snapshot eventually written");
        expect(IOException.class,()->s.begin(id,w.cart,new byte[1])); // pending? next case separately validates API below
    }
    static void integrity()throws Exception{
        Store s=store();FakeWire w=new FakeWire(6);String id=begin(s,w);byte[] next=w.ram.clone();next[0]=1;s.checkpoint(id,next);
        expect(IOException.class,()->s.begin(id,w.cart,w.ram));check(s.entry(id).pending(),"New session cannot overwrite pending save");
        Store.Entry e=s.entry(id);Store.atomic(new File(s.dir(id),e.save+".sav"),new byte[next.length]);expect(IOException.class,()->s.data(e));
    }
    static final class FakeBoot implements FirmwareImage.Boot {
        byte[] flash=new byte[2*1024*1024];boolean rebooted;int erases;boolean corrupt;
        FakeBoot(){Arrays.fill(flash,(byte)0x69);}
        public byte[] read(int addr,int n){byte[] b=Arrays.copyOfRange(flash,addr-0x10000000,addr-0x10000000+n);if(corrupt&&erases>0)b[0]^=1;return b;}
        public void erase(int addr,int n){Arrays.fill(flash,addr-0x10000000,addr-0x10000000+n,(byte)255);erases++;}
        public void write(int addr,byte[] b){System.arraycopy(b,0,flash,addr-0x10000000,b.length);}
        public void reboot(){rebooted=true;}
    }
    static void firmware(String path)throws Exception{
        byte[] uf2=Files.readAllBytes(Paths.get(path));FirmwareImage image=new FirmwareImage(uf2,FirmwareImage.SHA256);check(image.pages.size()==469,"Pinned official firmware block count");
        expect(IOException.class,()->new FirmwareImage(uf2,"wrong hash"));
        byte[] bad=uf2.clone();bad[28]^=1;final byte[] wrongFamily=bad;expect(IOException.class,()->new FirmwareImage(wrongFamily,Store.hash(wrongFamily))); // wrong chip family
        bad=uf2.clone();ByteBuffer.wrap(bad).order(ByteOrder.LITTLE_ENDIAN).putInt(12,0x20000000);final byte[] ramImage=bad;expect(IOException.class,()->new FirmwareImage(ramImage,Store.hash(ramImage)));
        FakeBoot boot=new FakeBoot();File backup=Files.createTempDirectory("gbfw-tests").resolve("before.gbfw").toFile();image.install(boot,backup,NO_PROGRESS);
        check(backup.isFile()&&backup.length()>90000,"Firmware backup created before writes");check(boot.rebooted,"Firmware reboot only after verification");
        for(Map.Entry<Integer,byte[]> p:image.pages.entrySet())check(Arrays.equals(p.getValue(),boot.read(p.getKey(),256)),"Firmware page matches");
        int last=image.pages.lastKey()+256;check(boot.flash[last-0x10000000]==0x69,"Uncovered sector bytes preserved");check(boot.flash[boot.flash.length-1]==0x69,"Adapter settings / other sectors preserved");
        FakeBoot faulty=new FakeBoot();faulty.corrupt=true;expect(IOException.class,()->image.install(faulty,new File(backup.getParentFile(),"failed.gbfw"),NO_PROGRESS));check(!faulty.rebooted,"No firmware success/reboot after verify mismatch");
    }
    public static void main(String[] args)throws Exception{codec();sizes();disconnect(false);disconnect(true);conflicts();newerDuringWrite();integrity();if(args.length>0)firmware(args[0]);System.out.println("PASS: "+checks+" integrity / protocol / disconnect / firmware checks");}
}
