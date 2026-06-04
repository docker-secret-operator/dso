"use client";

import React from "react";
import { motion } from "framer-motion";
import { LucideIcon } from "lucide-react";

interface StatCardProps {
  icon: LucideIcon;
  label: string;
  value?: string;
  delay?: number;
}

export const StatCard = ({
  icon: Icon,
  label,
  value,
  delay = 0,
}: StatCardProps) => {
  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true }}
      transition={{ duration: 0.5, delay }}
      className="flex items-center gap-3 px-4 py-3 rounded-lg border border-accent/20 bg-accent/5 hover:bg-accent/10 transition-colors"
    >
      <Icon className="w-5 h-5 text-accent flex-shrink-0" />
      <div className="flex flex-col gap-0.5">
        <p className="text-xs font-mono text-accent/70 uppercase tracking-wider">
          {label}
        </p>
        {value && <p className="text-sm font-semibold text-foreground">{value}</p>}
      </div>
    </motion.div>
  );
};
