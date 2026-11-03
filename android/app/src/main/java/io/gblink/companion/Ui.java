package io.gblink.companion;
import android.app.*;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.view.*;
import android.widget.*;

final class Ui {
    static final int BG=Color.rgb(16,19,28),CARD=Color.rgb(29,34,48),TEXT=Color.rgb(238,240,249),MUTED=Color.rgb(162,172,193),ACCENT=Color.rgb(174,157,255),GREEN=Color.rgb(119,226,182);
    static int dp(Activity a,int n){return (int)(a.getResources().getDisplayMetrics().density*n+.5f);}
    static GradientDrawable shape(int color,int radius){GradientDrawable d=new GradientDrawable();d.setColor(color);d.setCornerRadius(radius);return d;}
    static LinearLayout column(Activity a){LinearLayout v=new LinearLayout(a);v.setOrientation(LinearLayout.VERTICAL);return v;}
    static TextView text(Activity a,String s,int size,int color){TextView t=new TextView(a);t.setText(s);t.setTextColor(color);t.setTextSize(size);t.setPadding(0,dp(a,5),0,dp(a,5));return t;}
    static TextView heading(Activity a,String s,int size){TextView t=text(a,s,size,TEXT);t.setTypeface(null,Typeface.BOLD);return t;}
    static Button button(Activity a,String s,Runnable run){Button b=new Button(a);b.setText(s);b.setAllCaps(false);b.setTextColor(TEXT);b.setTextSize(14);b.setBackgroundTintList(android.content.res.ColorStateList.valueOf(Color.rgb(55,48,85)));b.setOnClickListener(v->run.run());return b;}
    static LinearLayout card(Activity a,LinearLayout parent){LinearLayout c=column(a);int p=dp(a,16);c.setPadding(p,p,p,p);c.setBackground(shape(CARD,dp(a,16)));LinearLayout.LayoutParams lp=new LinearLayout.LayoutParams(-1,-2);lp.setMargins(0,dp(a,7),0,dp(a,7));parent.addView(c,lp);return c;}
    static void error(Activity a,Exception e){if(!a.isFinishing())new AlertDialog.Builder(a).setTitle("Operation not completed").setMessage(e.getMessage()+"\n\nAny pending phone save is retained.").setPositiveButton("OK",null).show();}
}
