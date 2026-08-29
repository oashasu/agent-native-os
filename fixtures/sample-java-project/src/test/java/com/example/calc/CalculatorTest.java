package com.example.calc;

import static org.junit.Assert.assertEquals;

import org.junit.Test;

public class CalculatorTest {

    @Test
    public void addsTwoPositiveNumbers() {
        assertEquals(5, Calculator.add(2, 3));
    }

    @Test
    public void addsNegativeNumbers() {
        assertEquals(-2, Calculator.add(-1, -1));
    }
}
