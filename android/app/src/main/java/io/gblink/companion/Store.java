package io.gblink.companion;

import java.io.*;
import java.nio.file.*;
import java.security.*;
import java.util.*;

/** Durable, content-addressed save journal. ROMs and battery saves are separate. */
final class Store {
    final File root,roms,sessions,backups;
    Store(File root)throws IOException{
        this.root=root;roms=new File(root,"roms");sessions=new File(root,"sessions");backups=new File(root,"backups");
        for(File f:new File[]{roms,sessions,backups})if(!f.isDirectory()&&!f.mkdirs())throw new IOException("Cannot create save storage");
    }
    static String hash(byte[] b){
        try{byte[] d=MessageDigest.getInstance("SHA-256").digest(b);StringBuilder s=new StringBuilder();for(byte v:d)s.append(String.format(Locale.ROOT,"%02x",v&255));return s.toString();}
        catch(NoSuchAlgorithmException e){throw new AssertionError(e);}
    }
    static byte[] read(File f)throws IOException{return Files.readAllBytes(f.toPath());}
    static void atomic(File dest,byte[] data)throws IOException{
        File tmp=new File(dest.getParentFile(),dest.getName()+".tmp");
        try(FileOutputStream out=new FileOutputStream(tmp)){out.write(data);out.getFD().sync();}
        try{Files.move(tmp.toPath(),dest.toPath(),StandardCopyOption.ATOMIC_MOVE,StandardCopyOption.REPLACE_EXISTING);}
        catch(AtomicMoveNotSupportedException e){Files.move(tmp.toPath(),dest.toPath(),StandardCopyOption.REPLACE_EXISTING);}
    }
    static final class Entry {
        final String id,title,code,cart,base,save,allowed;
        final int type;final boolean interrupted;
        Entry(Properties p){id=p.getProperty("id");title=p.getProperty("title");code=p.getProperty("code");cart=p.getProperty("cart");base=p.getProperty("base");save=p.getProperty("save");allowed=p.getProperty("allowed",base);type=Integer.parseInt(p.getProperty("type"));interrupted=Boolean.parseBoolean(p.getProperty("interrupted","false"));}
        boolean pending(){return !Objects.equals(base,save)||interrupted;}
        boolean accepts(String hash){return Arrays.asList(allowed.split(",")).contains(hash);}
    }
    File dir(String id)throws IOException{if(id==null||!id.matches("[0-9a-f]{64}"))throw new IOException("Invalid ROM ID");return new File(sessions,id);}
    File meta(String id)throws IOException{return new File(dir(id),"session.properties");}
    synchronized Properties properties(String id)throws IOException{Properties p=new Properties();try(InputStream in=new FileInputStream(meta(id))){p.load(in);}return p;}
    synchronized Entry entry(String id)throws IOException{return new Entry(properties(id));}
    synchronized void put(String id,Properties p)throws IOException{ByteArrayOutputStream b=new ByteArrayOutputStream();p.store(b,"GB-Link cartridge save journal");atomic(meta(id),b.toByteArray());}
    synchronized byte[] data(Entry e)throws IOException{
        byte[] b=read(new File(dir(e.id),e.save+".sav"));
        if(!hash(b).equals(e.save)||b.length!=Wire.SIZES[e.type])throw new IOException("Phone save failed its integrity check; use an exported backup");return b;
    }
    synchronized Entry begin(String id,Wire.Cart cart,byte[] save)throws IOException{
        if(cart.type<1||cart.type>6||save.length!=Wire.SIZES[cart.type])throw new IOException("Cartridge save size does not match its chip");
        if(meta(id).exists()&&entry(id).pending())throw new IOException("This game has a pending phone save. Write it back or resume it first.");
        File dir=dir(id);if(!dir.exists()&&!dir.mkdirs())throw new IOException("Cannot create session");
        String sha=hash(save);atomic(new File(dir,sha+".sav"),save);backup(id,"cartridge-read",save);
        Properties p=new Properties();p.setProperty("id",id);p.setProperty("cart",cart.identity());p.setProperty("type",""+cart.type);p.setProperty("title",cart.title());p.setProperty("code",cart.code());p.setProperty("base",sha);p.setProperty("save",sha);p.setProperty("allowed",sha);put(id,p);return entry(id);
    }
    synchronized void checkpoint(String id,byte[] save)throws IOException{
        Entry e=entry(id);if(save.length!=Wire.SIZES[e.type])throw new IOException("Unexpected mGBA save size");
        String sha=hash(save);if(sha.equals(e.save))return;
        atomic(new File(dir(id),sha+".sav"),save);
        backup(id,"emulator-save",save);
        Properties p=properties(id);p.setProperty("save",sha);put(id,p);
    }
    synchronized void emulatorSaved(String id,byte[] save)throws IOException{
        // A real mGBA savedataUpdated callback, including rewrites of identical data.
        Entry e=entry(id);
        if(hash(save).equals(e.save))backup(id,"emulator-save",save);else checkpoint(id,save);
    }
    synchronized void prepareBlock(String id,byte[] predicted)throws IOException{
        Properties p=properties(id);String sha=hash(predicted), allowed=p.getProperty("allowed");
        if(!Arrays.asList(allowed.split(",")).contains(sha))p.setProperty("allowed",allowed+","+sha);
        p.setProperty("interrupted","true");put(id,p);
    }
    synchronized void complete(String id,String verifiedHash)throws IOException{
        Properties p=properties(id);p.setProperty("base",verifiedHash);p.setProperty("allowed",verifiedHash);p.setProperty("interrupted","false");put(id,p);
    }
    synchronized void backup(String id,String reason,byte[] save)throws IOException{
        atomic(new File(backups,id+"-"+System.currentTimeMillis()+"-"+reason+"-"+hash(save).substring(0,10)+"-"+UUID.randomUUID().toString().substring(0,8)+".sav"),save);
    }
    synchronized List<Entry> pending()throws IOException{
        List<Entry> list=new ArrayList<>();File[] dirs=sessions.listFiles();if(dirs!=null)for(File d:dirs)if(new File(d,"session.properties").exists()){Entry e=entry(d.getName());if(e.pending())list.add(e);}return list;
    }
    File rom(String id)throws IOException{dir(id);return new File(roms,id+".gba");}
    synchronized List<File> games(){File[] fs=roms.listFiles((d,n)->n.endsWith(".gba"));List<File> result=fs==null?new ArrayList<>():new ArrayList<>(Arrays.asList(fs));result.sort(Comparator.comparing(File::getName));return result;}
    static String id(File f){return f.getName().replace(".gba","");}
    synchronized File importRom(byte[] bytes)throws IOException{
        validateRom(bytes);File f=rom(hash(bytes));if(!f.exists())atomic(f,bytes);return f;
    }
    static void validateRom(byte[] bytes)throws IOException{
        if(bytes.length<192||bytes.length>32*1024*1024||bytes[0xB2]!=(byte)0x96)throw new IOException("Select an uncompressed GBA ROM (.gba), up to 32 MB.");
        int check=0;for(int i=0xA0;i<=0xBC;i++)check-=bytes[i]&255;
        if(((check-0x19)&255)!=(bytes[0xBD]&255))throw new IOException("GBA ROM header checksum is invalid");
    }
    static void matchRom(byte[] rom,Wire.Cart cart)throws IOException{
        validateRom(rom);
        if(!Arrays.equals(Arrays.copyOf(rom,192),cart.header))throw new IOException("Selected ROM does not match the cartridge header/revision. Choose its matching ROM.");
        if(cart.type==0)throw new IOException("This cartridge has no detected save chip. Cartridge-backed play requires a supported save chip.");
    }
}
