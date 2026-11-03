package io.gblink.companion;

import android.app.*;
import android.graphics.*;
import android.media.*;
import android.os.*;
import android.view.*;
import java.io.*;
import java.util.*;

public final class PlayActivity extends Activity {
    Hub hub;String id;GameView view;Engine engine;
    volatile int touchKeys,padKeys,axisKeys;boolean exiting,failed;
    @Override public void onCreate(Bundle b){
        super.onCreate(b);hub=Hub.get(this);id=getIntent().getStringExtra("id");
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        getWindow().getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_FULLSCREEN|View.SYSTEM_UI_FLAG_HIDE_NAVIGATION|View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY|View.SYSTEM_UI_FLAG_LAYOUT_STABLE);
        view=new GameView();setContentView(view);
        try{
            if(hub.activeId!=null)throw new IOException("Another game session is already running");
            Store.Entry e=hub.store.entry(id);byte[] rom=Store.read(hub.store.rom(id));
            if(!Store.hash(rom).equals(id))throw new IOException("Stored ROM failed its integrity check. Reimport it.");
            if(!Mgba.load(rom,hub.store.data(e),e.type))throw new IOException("mGBA could not load the ROM and cartridge save");
            hub.activeId=id;engine=new Engine();engine.start();
        }catch(Exception|UnsatisfiedLinkError e){failed=true;new AlertDialog.Builder(this).setTitle("Unable to start mGBA").setMessage(e.getMessage()).setPositiveButton("Return home",(d,w)->finish()).setCancelable(false).show();}
    }
    @Override public void onResume(){super.onResume();hub.listener=()->view.postInvalidate();if(engine!=null&&!exiting)engine.resumeGame();}
    @Override public void onPause(){
        hub.listener=null;touchKeys=padKeys=axisKeys=0;
        if(engine!=null){try{engine.pauseAndSave();}catch(IOException e){hub.lastError="Could not checkpoint phone save: "+e.getMessage();failed=true;}}
        super.onPause();
    }
    @Override public void onDestroy(){if(engine!=null){engine.stopGame();engine=null;}hub.activeId=null;super.onDestroy();}
    @Override public void onBackPressed(){exitGame();}
    void exitGame(){
        if(exiting||engine==null)return;exiting=true;
        try{engine.pauseAndSave();Store.Entry e=hub.store.entry(id);
            if(!e.pending()){finish();return;}
            final boolean canWrite=hub.ready&&!hub.busy;
            new AlertDialog.Builder(this).setTitle("Write save back to cartridge?")
                .setMessage(hub.ready?"The latest in-game save is saved on your phone. Write and verify it on the cartridge before returning home?":"GB-Link is disconnected. Your latest in-game save is retained on this phone. Return home, reconnect GB-Link and start the GBA helper to write it back.")
                .setPositiveButton(canWrite?"Write & exit":"Home · save pending",(d,w)->{if(canWrite&&hub.ready&&!hub.busy)exitSync(null);else finish();})
                .setNegativeButton("Exit · keep pending",(d,w)->finish())
                .setNeutralButton("Keep playing",(d,w)->continueGame()).setOnCancelListener(d->continueGame()).show();
        }catch(IOException e){exiting=false;Ui.error(this,e);}
    }
    void continueGame(){exiting=false;if(engine!=null)engine.resumeGame();}
    void exitSync(String force){
        hub.sync(id,force,error->{
            if(error==null){finish();return;}
            if(error instanceof CartridgeSync.Conflict){CartridgeSync.Conflict c=(CartridgeSync.Conflict)error;
                new AlertDialog.Builder(this).setTitle("Cartridge save changed")
                    .setMessage(c.getMessage()+"\nBack up and replace it with this phone save?")
                    .setPositiveButton("Back up & replace",(d,w)->exitSync(c.currentHash))
                    .setNegativeButton("Home · keep pending",(d,w)->finish()).setNeutralButton("Keep playing",(d,w)->continueGame()).setOnCancelListener(d->continueGame()).show();
            }else new AlertDialog.Builder(this).setTitle("Save still pending").setMessage(error.getMessage()+"\nYour phone copy is retained. Reconnect and retry from the main screen.")
                .setPositiveButton("Return home",(d,w)->finish()).setNegativeButton("Keep playing",(d,w)->continueGame()).setOnCancelListener(d->continueGame()).show();
        });
    }
    final class Engine extends Thread {
        final Object lock=new Object();volatile boolean running=true,paused,closed;
        final int[] frame=new int[240*160];final short[] pcm=new short[8192];
        AudioTrack audio;int frames;String observed="";long changedAt;
        Engine(){super("mGBA emulation");}
        public void run(){
            try{
                int buffer=Math.max(8192,AudioTrack.getMinBufferSize(32768,AudioFormat.CHANNEL_OUT_STEREO,AudioFormat.ENCODING_PCM_16BIT));
                audio=new AudioTrack.Builder().setAudioAttributes(new AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_GAME).setContentType(AudioAttributes.CONTENT_TYPE_MUSIC).build())
                    .setAudioFormat(new AudioFormat.Builder().setEncoding(AudioFormat.ENCODING_PCM_16BIT).setSampleRate(32768).setChannelMask(AudioFormat.CHANNEL_OUT_STEREO).build())
                    .setBufferSizeInBytes(buffer).setTransferMode(AudioTrack.MODE_STREAM).build();audio.play();
            }catch(Exception e){audio=null;hub.lastError="Audio unavailable: "+e.getMessage();}
            long next=System.nanoTime();boolean audioPaused=false;
            try{
                while(running){
                    if(paused){if(audio!=null&&!audioPaused){audio.pause();audio.flush();audioPaused=true;}Thread.sleep(20);next=System.nanoTime();continue;}
                    if(audio!=null&&audioPaused){audio.play();audioPaused=false;}
                    int n;
                    synchronized(lock){if(paused||!running)continue;n=Mgba.step(touchKeys|padKeys|axisKeys|gbaToRetro(hub.gbaKeys),frame,pcm);
                        int saves=Mgba.saveEvents();if(saves>0){byte[] data=Mgba.save();for(int i=0;i<saves;i++)hub.store.emulatorSaved(id,data);hub.autoSync(id);}
                        if(++frames%30==0)checkpointStable();}
                    if(audio!=null&&n>0)audio.write(pcm,0,n,AudioTrack.WRITE_NON_BLOCKING);
                    synchronized(view.bitmap){view.bitmap.setPixels(frame,0,240,0,0,240,160);}view.postInvalidateOnAnimation();
                    if(frames%6==0)hub.pollKeys();
                    next+=16_742_706L;long wait=next-System.nanoTime();if(wait>0)Thread.sleep(wait/1_000_000,(int)(wait%1_000_000));else if(wait < -100_000_000L)next=System.nanoTime();
                }
            }catch(Exception e){
                if(running){hub.lastError="mGBA stopped: "+e.getMessage();runOnUiThread(()->{if(!isFinishing())new AlertDialog.Builder(PlayActivity.this).setTitle("Game paused").setMessage(hub.lastError+"\nReturn home to review the retained save.").setPositiveButton("Home",(d,w)->finish()).setCancelable(false).show();});}
            }finally{
                synchronized(lock){try{hub.store.checkpoint(id,Mgba.save());}catch(Exception e){hub.lastError="Phone save checkpoint failed: "+e.getMessage();}Mgba.close();closed=true;}
                if(audio!=null){audio.stop();audio.release();}
            }
        }
        void checkpointStable()throws IOException{
            byte[] save=Mgba.save();String sha=Store.hash(save);long now=System.nanoTime();
            if(!sha.equals(observed)){observed=sha;changedAt=now;return;}
            if(now-changedAt>=1_500_000_000L){hub.store.checkpoint(id,save);hub.autoSync(id);}
        }
        void pauseAndSave()throws IOException{synchronized(lock){paused=true;if(!closed)hub.store.checkpoint(id,Mgba.save());}}
        void resumeGame(){paused=false;}
        void stopGame(){running=false;interrupt();try{join(3000);}catch(InterruptedException e){Thread.currentThread().interrupt();}}
    }
    static int gbaToRetro(int k){
        int r=0;int[] map={8,0,2,3,7,6,4,5,11,10};for(int i=0;i<10;i++)if((k&(1<<i))!=0)r|=1<<map[i];return r;
    }
    static int key(int code){
        switch(code){
        case KeyEvent.KEYCODE_BUTTON_A:return 1<<8;case KeyEvent.KEYCODE_BUTTON_B:return 1;
        case KeyEvent.KEYCODE_BUTTON_X:return 1;case KeyEvent.KEYCODE_BUTTON_Y:return 1<<8;
        case KeyEvent.KEYCODE_DPAD_UP:return 1<<4;case KeyEvent.KEYCODE_DPAD_DOWN:return 1<<5;
        case KeyEvent.KEYCODE_DPAD_LEFT:return 1<<6;case KeyEvent.KEYCODE_DPAD_RIGHT:return 1<<7;
        case KeyEvent.KEYCODE_BUTTON_L1:case KeyEvent.KEYCODE_BUTTON_L2:return 1<<10;
        case KeyEvent.KEYCODE_BUTTON_R1:case KeyEvent.KEYCODE_BUTTON_R2:return 1<<11;
        case KeyEvent.KEYCODE_BUTTON_START:return 1<<3;case KeyEvent.KEYCODE_BUTTON_SELECT:return 1<<2;
        default:return 0;
        }
    }
    @Override public boolean dispatchKeyEvent(KeyEvent e){int bit=key(e.getKeyCode());if(bit!=0){if(e.getAction()==KeyEvent.ACTION_DOWN)padKeys|=bit;else if(e.getAction()==KeyEvent.ACTION_UP)padKeys&=~bit;return true;}return super.dispatchKeyEvent(e);}
    @Override public boolean onGenericMotionEvent(MotionEvent e){
        if((e.getSource()&InputDevice.SOURCE_JOYSTICK)==InputDevice.SOURCE_JOYSTICK){
            float x=e.getAxisValue(MotionEvent.AXIS_X)+e.getAxisValue(MotionEvent.AXIS_HAT_X),y=e.getAxisValue(MotionEvent.AXIS_Y)+e.getAxisValue(MotionEvent.AXIS_HAT_Y);
            axisKeys=(x<-.5?1<<6:0)|(x>.5?1<<7:0)|(y<-.5?1<<4:0)|(y>.5?1<<5:0);return true;
        }return super.onGenericMotionEvent(e);
    }
    final class GameView extends View {
        final Bitmap bitmap=Bitmap.createBitmap(240,160,Bitmap.Config.ARGB_8888);final Paint paint=new Paint();
        final List<Control> controls=new ArrayList<>();final RectF screen=new RectF();RectF home=new RectF();
        final class Control{String name;int bit;RectF rect;Control(String n,int b,RectF r){name=n;bit=b;rect=r;}}
        GameView(){super(PlayActivity.this);setFocusable(true);setFocusableInTouchMode(true);}
        void control(String name,int bit,float x,float y,float w,float h){controls.add(new Control(name,bit,new RectF(x,y,x+w,y+h)));}
        @Override protected void onSizeChanged(int w,int h,int oldw,int oldh){
            float unit=Math.min(w*.065f,h*.14f),cx=w*.105f,cy=h*.59f;
            controls.clear();
            control("↑",1<<4,cx-unit/2,cy-unit*1.5f,unit,unit);control("↓",1<<5,cx-unit/2,cy+unit*.5f,unit,unit);
            control("←",1<<6,cx-unit*1.5f,cy-unit/2,unit,unit);control("→",1<<7,cx+unit*.5f,cy-unit/2,unit,unit);
            control("A",1<<8,w-unit*1.2f,h*.40f,unit,unit);control("B",1,w-unit*2.3f,h*.60f,unit,unit);
            control("L",1<<10,w*.025f,h*.10f,w*.14f,h*.12f);control("R",1<<11,w*.835f,h*.10f,w*.14f,h*.12f);
            control("Select",1<<2,w*.32f,h*.86f,w*.14f,h*.09f);control("Start",1<<3,w*.54f,h*.86f,w*.14f,h*.09f);
            float sw=Math.min(w*.60f,h*.78f*1.5f),sh=sw/1.5f;screen.set((w-sw)/2,h*.04f,(w+sw)/2,h*.04f+sh);
            home.set(w*.89f,h*.86f,w*.99f,h*.97f);
        }
        @Override protected void onDraw(Canvas canvas){
            canvas.drawColor(Ui.BG);paint.setFilterBitmap(false);synchronized(bitmap){canvas.drawBitmap(bitmap,null,screen,paint);}
            paint.setTextAlign(Paint.Align.CENTER);paint.setTypeface(Typeface.DEFAULT_BOLD);paint.setTextSize(Math.max(18,getHeight()*.036f));
            int held=touchKeys|padKeys|axisKeys;
            for(Control c:controls){paint.setColor((held&c.bit)!=0?0xFF6C5CA6:Ui.CARD);canvas.drawRoundRect(c.rect,16,16,paint);paint.setColor(Ui.TEXT);canvas.drawText(c.name,c.rect.centerX(),c.rect.centerY()-(paint.ascent()+paint.descent())/2,paint);}
            paint.setColor(Ui.CARD);canvas.drawRoundRect(home,14,14,paint);paint.setColor(Ui.TEXT);canvas.drawText("Home",home.centerX(),home.centerY()-(paint.ascent()+paint.descent())/2,paint);
            paint.setTextSize(Math.max(13,getHeight()*.025f));paint.setColor(hub.ready?Ui.GREEN:Ui.ACCENT);
            String label=hub.ready?(hub.busy?"Writing / verifying save…":"mGBA · cartridge connected"):"mGBA · unplugged · write-back on exit";
            canvas.drawText(label,getWidth()/2f,getHeight()*.985f,paint);
        }
        @Override public boolean onTouchEvent(MotionEvent e){
            int action=e.getActionMasked();
            if(action==MotionEvent.ACTION_CANCEL){touchKeys=0;invalidate();return true;}
            if(action==MotionEvent.ACTION_DOWN&&home.contains(e.getX(),e.getY())){touchKeys=0;exitGame();return true;}
            int keys=0;
            for(int i=0;i<e.getPointerCount();i++){
                if((action==MotionEvent.ACTION_UP||action==MotionEvent.ACTION_POINTER_UP)&&i==e.getActionIndex())continue;
                for(Control c:controls)if(c.rect.contains(e.getX(i),e.getY(i)))keys|=c.bit;
            }
            touchKeys=keys;invalidate();return true;
        }
    }
}
